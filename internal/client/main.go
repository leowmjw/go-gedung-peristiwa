package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"sync"
	"time"

	"app/internal/shared"

	"github.com/VictoriaMetrics/easyproto"
	"google.golang.org/grpc"
)

// MetricsRequest represents a gRPC message containing metric data
type MetricsRequest struct {
	Data []byte `protobuf:"bytes,1,opt,name=data,proto3" json:"data,omitempty"`
}

// Reset implements proto.Message
func (m *MetricsRequest) Reset() {
	*m = MetricsRequest{}
}

// String implements proto.Message
func (m *MetricsRequest) String() string {
	return fmt.Sprintf("%+v", *m)
}

// ProtoMessage implements proto.Message
func (m *MetricsRequest) ProtoMessage() {}

// MetricsResponse represents a gRPC response message
type MetricsResponse struct {
	Data []byte `protobuf:"bytes,1,opt,name=data,proto3" json:"data,omitempty"`
}

// Reset implements proto.Message
func (m *MetricsResponse) Reset() {
	*m = MetricsResponse{}
}

// String implements proto.Message
func (m *MetricsResponse) String() string {
	return fmt.Sprintf("%+v", *m)
}

// ProtoMessage implements proto.Message
func (m *MetricsResponse) ProtoMessage() {}

func marshalMetricPoint(mp *shared.MetricPoint, pool *sync.Pool) []byte {
	m := pool.Get().(*easyproto.Marshaler)
	defer func() {
		m.Reset()
		pool.Put(m)
	}()

	mm := m.MessageMarshaler()
	mm.AppendInt64(1, mp.Timestamp)
	mm.AppendDouble(2, mp.Value)
	mm.AppendBool(3, mp.IsNull)

	for _, label := range mp.Labels {
		labelMsg := mm.AppendMessage(4)
		labelMsg.AppendString(1, label.Name)
		labelMsg.AppendString(2, label.Value)
	}

	if mp.Properties != nil {
		kvMsg := mm.AppendMessage(5)
		for k, v := range mp.Properties.Items {
			itemMsg := kvMsg.AppendMessage(1)
			itemMsg.AppendString(1, k)
			marshalValue(itemMsg.AppendMessage(2), &v)
		}
	}

	return m.Marshal(nil)
}

func marshalValue(mm *easyproto.MessageMarshaler, v *shared.Value) {
	mm.AppendInt32(1, int32(v.Type))

	switch v.Type {
	case shared.ValueType_INT:
		if v.IntVal != nil {
			mm.AppendInt64(2, *v.IntVal)
		}
	case shared.ValueType_FLOAT:
		if v.FloatVal != nil {
			mm.AppendDouble(3, *v.FloatVal)
		}
	case shared.ValueType_STRING:
		if v.StringVal != nil {
			mm.AppendString(4, *v.StringVal)
		}
	case shared.ValueType_LIST:
		for _, item := range v.ListVal {
			itemCopy := item
			marshalValue(mm.AppendMessage(5), &itemCopy)
		}
	}
}

func generateRandomMetric() shared.MetricPoint {
	// Random values for properties
	intVal := rand.Int63n(1000)
	floatVal := rand.Float64() * 100
	strVals := []string{"test", "prod", "dev", "staging"}
	strVal := strVals[rand.Intn(len(strVals))]

	// Random environments and hosts
	envs := []string{"prod", "staging", "dev", "test"}
	hosts := []string{"server1", "server2", "server3", "server4", "server5"}

	// Decide if this will be a simple or complex metric
	isComplex := rand.Float32() < 0.4 // 40% chance of complex metric

	metric := shared.MetricPoint{
		Timestamp: time.Now().UnixNano(),
		Value:     rand.Float64() * 1000,
		IsNull:    rand.Float32() < 0.1, // 10% chance of null
		Labels: []shared.Label{
			{Name: "host", Value: hosts[rand.Intn(len(hosts))]},
			{Name: "env", Value: envs[rand.Intn(len(envs))]},
		},
		Properties: &shared.KeyValue{
			Items: map[string]shared.Value{
				"count": {
					Type:   shared.ValueType_INT,
					IntVal: &intVal,
				},
			},
		},
	}

	if isComplex {
		// Add more labels
		extraLabels := []struct{ name, value string }{
			{"region", "us-west"},
			{"datacenter", "dc1"},
			{"service", "api"},
			{"version", "v1.2.3"},
		}
		for _, label := range extraLabels {
			if rand.Float32() < 0.7 { // 70% chance to include each extra label
				metric.Labels = append(metric.Labels, shared.Label{
					Name:  label.name,
					Value: label.value,
				})
			}
		}

		// Add more properties
		metric.Properties.Items["ratio"] = shared.Value{
			Type:     shared.ValueType_FLOAT,
			FloatVal: &floatVal,
		}
		metric.Properties.Items["tags"] = shared.Value{
			Type: shared.ValueType_LIST,
			ListVal: []shared.Value{
				{
					Type:      shared.ValueType_STRING,
					StringVal: &strVal,
				},
			},
		}
	}

	return metric
}

func sendMetricsBatch(ctx context.Context, metrics []shared.MetricPoint, stream grpc.ClientStream, pool *sync.Pool, wg *sync.WaitGroup) {
	defer wg.Done()

	for _, metric := range metrics {
		// Get marshaler from pool
		marshaler := pool.Get().(*easyproto.Marshaler)
		data := marshalMetricPoint(&metric, pool)
		pool.Put(marshaler)

		log.Printf("Sending metric with data length: %d", len(data))
		req := &MetricsRequest{Data: data}
		if err := stream.SendMsg(req); err != nil {
			if err == io.EOF {
				log.Printf("Stream closed by server")
				return
			}
			log.Printf("Failed to send metric: %v", err)
			continue
		}

		// Get response
		resp := &MetricsResponse{}
		if err := stream.RecvMsg(resp); err != nil {
			if err == io.EOF {
				log.Printf("Stream closed by server")
				return
			}
			log.Printf("Failed to receive response: %v", err)
			continue
		}
		log.Printf("Received response with data length: %d", len(resp.Data))
	}
}

func main() {
	// Parse command line flags
	flag.Parse()

	// Create a pool for marshalers
	pool := &sync.Pool{
		New: func() any {
			return &easyproto.Marshaler{}
		},
	}

	// Generate random number of metrics between 10 and 25
	numMetrics := rand.Intn(16) + 10 // 10 to 25 inclusive
	log.Printf("Generating %d metrics", numMetrics)

	// Generate random metrics
	metrics := make([]shared.MetricPoint, numMetrics)
	for i := 0; i < numMetrics; i++ {
		metrics[i] = generateRandomMetric()
	}

	// Set up gRPC connection
	conn, err := grpc.Dial("localhost"+shared.ServerAddress, grpc.WithInsecure())
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Create stream
	ctx := context.Background()
	stream, err := conn.NewStream(ctx, &grpc.StreamDesc{
		StreamName:    shared.MethodName,
		ServerStreams: true,
		ClientStreams: true,
	}, shared.FullMethodPath)
	if err != nil {
		log.Fatalf("Failed to create stream: %v", err)
	}

	// Send metrics in batches
	batchSize := 5
	var wg sync.WaitGroup

	for i := 0; i < len(metrics); i += batchSize {
		end := i + batchSize
		if end > len(metrics) {
			end = len(metrics)
		}

		wg.Add(1)
		go sendMetricsBatch(ctx, metrics[i:end], stream, pool, &wg)
	}

	wg.Wait()

	// Close the stream gracefully
	if err := stream.CloseSend(); err != nil {
		log.Printf("Error closing stream: %v", err)
	}
	log.Printf("All metrics sent successfully")
}
