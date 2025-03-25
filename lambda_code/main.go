package main

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/credentials/stscreds"
	"github.com/prometheus/prometheus/prompb"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	v4 "github.com/aws/aws-sdk-go/aws/signer/v4"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
)

type MetricStreamData struct {
	MetricStreamName string     `json:"metric_stream_name"`
	AccountID        string     `json:"account_id"`
	Region           string     `json:"region"`
	Namespace        string     `json:"namespace"`
	MetricName       string     `json:"metric_name"`
	Dimensions       Dimensions `json:"dimensions"`
	Timestamp        int64      `json:"timestamp"`
	Value            Value      `json:"value"`
	Unit             string     `json:"unit"`
}

type Dimensions = map[string]interface{}

//	type Dimensions struct {
//		Class    string `json:"Class"`
//		Resource string `json:"Resource"`
//		Service  string `json:"Service"`
//		Type     string `json:"Type"`
//	}
type Value struct {
	Count float64 `json:"count"`
	Sum   float64 `json:"sum"`
	Max   float64 `json:"max"`
	Min   float64 `json:"min"`
}

type Values string

var rex = regexp.MustCompile("(\\w+):(\\w+)")

const (
	Count Values = "count"
	Sum          = "sum"
	Max          = "max"
	Min          = "min"
)

func HandleRequest(ctx context.Context, firehoseEvent events.KinesisFirehoseEvent) (events.KinesisFirehoseResponse, error) {

	var response events.KinesisFirehoseResponse
	var timeSeries []prompb.TimeSeries
	// These are the 4 value types from Cloudwatch, each of which map to a Prometheus Gauge
	values := []Values{Count, Max, Min, Sum}

	for _, record := range firehoseEvent.Records {

		splitRecord := strings.Split(string(record.Data), string('\n'))
		for _, x := range splitRecord {

			// The Records includes an empty new line at the last position which becomes "" after parsing. Skipping over the empty string.
			if x == "" {
				continue
			}
			var metricStreamData MetricStreamData
			err := json.Unmarshal([]byte(x), &metricStreamData)
			if err != nil {
				panic(err)
			}

			// For each metric, the labels + valuetype is the __name__ of the sample, and the corresponding single sample value is used to create the timeseries.
			for _, value := range values {
				var samples []prompb.Sample
				currentLabels := handleAddLabels(value, metricStreamData.MetricName, metricStreamData.Namespace, metricStreamData.Dimensions)
				currentSamples := handleAddSamples(value, metricStreamData.Value, metricStreamData.Timestamp)
				samples = append(samples, currentSamples)

				singleTimeSeries := prompb.TimeSeries{
					Labels:  currentLabels,
					Samples: samples,
				}
				timeSeries = append(timeSeries, singleTimeSeries)
			}
		}

		// No transformation occurs, just send OK response back to Kinesis
		var transformedRecord events.KinesisFirehoseResponseRecord
		transformedRecord.RecordID = record.RecordID
		transformedRecord.Result = events.KinesisFirehoseTransformedStateOk
		transformedRecord.Data = record.Data

		response.Records = append(response.Records, transformedRecord)
	}

	_, err := createWriteRequestAndSendToAPS(timeSeries)
	if err != nil {
		panic(err)
	}
	return response, nil
}

func main() {
	lambda.Start(HandleRequest)
}

// Taken directly from YACE: https://github.com/nerdswords/yet-another-cloudwatch-exporter/blob/1c7b3d7b7b64ce93bb4a27d8ef836e0c2b96b8e7/pkg/prometheus.go#L139
func sanitize(text string) string {
	replacer := strings.NewReplacer(
		" ", "_",
		",", "_",
		"\t", "_",
		"/", "_",
		"\\", "_",
		".", "_",
		"-", "_",
		":", "_",
		"=", "_",
		"“", "_",
		"@", "_",
		"<", "_",
		">", "_",
		"%", "_percent",
	)
	return replacer.Replace(text)
}

func handleAddLabels(valueType Values, metricName string, namespace string, dimensions Dimensions) []prompb.Label {

	var labels []prompb.Label

	metricNameLabel := createMetricNameLabel(metricName, valueType)
	namespaceLabel := createNamespaceLabel(namespace)
	dimensionLabels := createDimensionLabels(dimensions)
	awsAccountLabels := createExtraLabels(os.Getenv("EXTRA_LABELS"))
	labels = append(labels, dimensionLabels...)
	labels = append(labels, awsAccountLabels...)
	labels = append(labels, metricNameLabel, namespaceLabel)

	return labels
}

func handleAddSamples(valueType Values, value Value, timestamp int64) prompb.Sample {
	var sample prompb.Sample
	switch valueType {
	case Count:
		sample = createCountSample(value, timestamp)
	case Min:
		sample = createMinSample(value, timestamp)
	case Max:
		sample = createMaxSample(value, timestamp)
	case Sum:
		sample = createSumSample(value, timestamp)
	}
	return sample
}

func createMetricNameLabel(metricName string, valueType Values) prompb.Label {
	metricNameLabel := prompb.Label{
		Name:  "__name__",
		Value: sanitize(metricName) + "_" + string(valueType),
	}
	return metricNameLabel
}

func createNamespaceLabel(namespace string) prompb.Label {
	namespaceLabel := prompb.Label{
		Name:  "namespace",
		Value: sanitize(namespace),
	}
	return namespaceLabel
}

func createExtraLabels(labelsFromEnv string) []prompb.Label {
	var labels []prompb.Label

	data := rex.FindAllStringSubmatch(labelsFromEnv, -1)

	// create one label for each kev-value
	for _, kv := range data {
		dimLabel := prompb.Label{
			Name:  sanitize(kv[1]),
			Value: sanitize(kv[2]),
		}
		labels = append(labels, dimLabel)
	}

	return labels
}

func createDimensionLabels(dimensions Dimensions) []prompb.Label {
	var labels []prompb.Label

	// create one label for each dimension
	for key, value := range dimensions {
		dimLabel := prompb.Label{
			Name:  sanitize(key),
			Value: sanitize(value.(string)),
		}
		labels = append(labels, dimLabel)
	}

	return labels
}

func createSumSample(value Value, timestamp int64) prompb.Sample {
	sumSample := prompb.Sample{
		Value:     value.Sum,
		Timestamp: timestamp,
	}
	return sumSample
}

func createCountSample(value Value, timestamp int64) prompb.Sample {
	countSample := prompb.Sample{
		Value:     value.Count,
		Timestamp: timestamp,
	}
	return countSample
}

func createMaxSample(value Value, timestamp int64) prompb.Sample {
	maxSample := prompb.Sample{
		Value:     value.Max,
		Timestamp: timestamp,
	}
	return maxSample
}

func createMinSample(value Value, timestamp int64) prompb.Sample {
	minSample := prompb.Sample{
		Value:     value.Min,
		Timestamp: timestamp,
	}
	return minSample
}

func createWriteRequestAndSendToAPS(timeseries []prompb.TimeSeries) (*http.Response, error) {
	writeRequest := &prompb.WriteRequest{
		Timeseries: timeseries,
	}

	body := encodeWriteRequestIntoProtoAndSnappy(writeRequest)
	response, err := sendRequestToAPS(body)
	return response, err
}

func encodeWriteRequestIntoProtoAndSnappy(writeRequest *prompb.WriteRequest) *bytes.Reader {
	data, err := proto.Marshal(writeRequest)

	if err != nil {
		panic(err)
	}

	encoded := snappy.Encode(nil, data)
	body := bytes.NewReader(encoded)
	return body
}

func roleSessionName() string {
	suffix, err := os.Hostname()

	if err != nil {
		now := time.Now().Unix()
		suffix = strconv.FormatInt(now, 10)
	}

	return "aws-sigv4-proxy-" + suffix
}

func sendRequestToAPS(body *bytes.Reader) (*http.Response, error) {
	// Create an HTTP request from the body content and set necessary parameters.
	req, err := http.NewRequest("POST", os.Getenv("PROMETHEUS_REMOTE_WRITE_URL"), body)
	if err != nil {
		panic(err)
	}

	sess, _ := session.NewSession(&aws.Config{
		Region: aws.String(os.Getenv("AWS_REGION")),
	})

	roleArn := os.Getenv("AWS_AMP_ROLE_ARN")

	var awsCredentials *credentials.Credentials
	if roleArn != "" {
		awsCredentials = stscreds.NewCredentials(sess, roleArn, func(p *stscreds.AssumeRoleProvider) {
			p.RoleSessionName = roleSessionName()
		})
	} else {
		awsCredentials = sess.Config.Credentials
	}

	signer := v4.NewSigner(awsCredentials)

	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("Content-Encoding", "snappy")
	req.Header.Set("X-Prometheus-Remote-Write-Version", "0.1.0")

	_, err = signer.Sign(req, body, "aps", os.Getenv("PROMETHEUS_REGION"), time.Now())

	if err != nil {
		panic(err)
	}

	resp, err := http.DefaultClient.Do(req)

	if resp.StatusCode != http.StatusOK {
		log.Println("Request to AMP failed with status: ", resp.StatusCode)
	}

	if err != nil {
		panic(err)
	}
	return resp, err
}