package main

import (
	"context"
	"fmt"
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
		data := marshalMetricPoint(&metric, pool)
		log.Printf("Sending metric with data length: %d", len(data))
		req := &MetricsRequest{Data: data}

		if err := stream.SendMsg(req); err != nil {
			log.Printf("Failed to send metric: %v", err)
			return
		}

		resp := &MetricsResponse{}
		if err := stream.RecvMsg(resp); err != nil {
			log.Printf("Failed to receive response: %v", err)
			return
		}

		log.Printf("Received response with data length: %d", len(resp.Data))

		var fc easyproto.FieldContext
		data = resp.Data
		var success bool
		var message string

		for len(data) > 0 {
			var err error
			data, err = fc.NextField(data)
			if err != nil {
				log.Printf("Failed to parse response: %v", err)
				return
			}

			switch fc.FieldNum {
			case 1: // Success flag
				if v, ok := fc.Bool(); ok {
					success = v
				}
			case 2: // Message
				if v, ok := fc.String(); ok {
					message = v
				}
			}
		}

		log.Printf("Response: success=%v, message=%s", success, message)
	}
}

func main() {
	rand.Seed(time.Now().UnixNano())

	pool := &sync.Pool{
		New: func() interface{} {
			return &easyproto.Marshaler{}
		},
	}

	conn, err := grpc.Dial("localhost"+shared.ServerAddress, grpc.WithInsecure())
	if err != nil {
		log.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Generate random number of metrics between 10 and 25
	numMetrics := 10 + rand.Intn(16)
	log.Printf("Generating %d metrics", numMetrics)

	// Generate all metrics
	var metrics []shared.MetricPoint
	for i := 0; i < numMetrics; i++ {
		metrics = append(metrics, generateRandomMetric())
	}

	// Create streams for concurrent batches
	var streams []grpc.ClientStream
	numStreams := (len(metrics) + 9) / 10 // Ceiling division to get number of needed streams
	for i := 0; i < numStreams; i++ {
		stream, err := conn.NewStream(ctx, &grpc.StreamDesc{
			StreamName:    shared.MethodName,
			ServerStreams: true,
			ClientStreams: true,
		}, shared.FullMethodPath)
		if err != nil {
			log.Fatalf("Failed to create stream %d: %v", i, err)
		}
		streams = append(streams, stream)
		defer func(s grpc.ClientStream) {
			if err := s.CloseSend(); err != nil {
				log.Printf("Error closing stream: %v", err)
			}
		}(stream)
	}

	// Send metrics in batches concurrently
	var wg sync.WaitGroup
	batchSize := 5 + rand.Intn(6) // Random batch size between 5 and 10
	
	for i := 0; i < len(metrics); i += batchSize {
		end := i + batchSize
		if end > len(metrics) {
			end = len(metrics)
		}
		
		batch := metrics[i:end]
		streamIndex := i / batchSize
		
		wg.Add(1)
		go sendMetricsBatch(ctx, batch, streams[streamIndex], pool, &wg)
	}

	// Wait for all batches to complete
	wg.Wait()
	log.Printf("All metrics sent successfully")
}
