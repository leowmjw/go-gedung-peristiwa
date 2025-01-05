package main

import (
	"context"
	"fmt"
	"log"
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

func main() {
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
	defer cancel() // This will gracefully cancel the stream when we're done

	stream, err := conn.NewStream(ctx, &grpc.StreamDesc{
		StreamName:    shared.MethodName,
		ServerStreams: true,
		ClientStreams: true,
	}, shared.FullMethodPath)
	if err != nil {
		log.Fatalf("failed to create stream: %v", err)
	}

	// Create sample metrics
	intVal := int64(42)
	floatVal := 3.14
	strVal := "test"

	metrics := []shared.MetricPoint{
		{
			Timestamp: time.Now().UnixNano(),
			Value:     123.456,
			IsNull:    false,
			Labels: []shared.Label{
				{Name: "host", Value: "server1"},
				{Name: "env", Value: "prod"},
			},
			Properties: &shared.KeyValue{
				Items: map[string]shared.Value{
					"count": {
						Type:   shared.ValueType_INT,
						IntVal: &intVal,
					},
					"ratio": {
						Type:     shared.ValueType_FLOAT,
						FloatVal: &floatVal,
					},
					"tags": {
						Type: shared.ValueType_LIST,
						ListVal: []shared.Value{
							{
								Type:      shared.ValueType_STRING,
								StringVal: &strVal,
							},
						},
					},
				},
			},
		},
	}

	// Send metrics
	for _, metric := range metrics {
		data := marshalMetricPoint(&metric, pool)
		log.Printf("Sending metric with data length: %d", len(data))
		req := &MetricsRequest{Data: data}

		if err := stream.SendMsg(req); err != nil {
			log.Fatalf("failed to send metric: %v", err)
		}

		resp := &MetricsResponse{}
		if err := stream.RecvMsg(resp); err != nil {
			log.Fatalf("failed to receive response: %v", err)
		}

		log.Printf("Received response with data length: %d", len(resp.Data))

		var fc easyproto.FieldContext
		data = resp.Data
		var success bool
		var message string

		for len(data) > 0 {
			data, err = fc.NextField(data)
			if err != nil {
				log.Fatalf("failed to parse response: %v", err)
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

	// Gracefully close the stream
	if err := stream.CloseSend(); err != nil {
		log.Printf("Error closing stream: %v", err)
	}
}
