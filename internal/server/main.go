package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"app/internal/shared"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/VictoriaMetrics/easyproto"
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

type MetricsService struct {
	grpc.ServerStream
	marshalerPool sync.Pool
}

func (s *MetricsService) ProcessMetrics(stream grpc.ServerStream) error {
	// Get the context from the stream
	ctx := stream.Context()

	for {
		select {
		case <-ctx.Done():
			// Client has disconnected or context was canceled
			log.Printf("Client disconnected: %v", ctx.Err())
			return nil
		default:
			// Read incoming message with a timeout
			readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			req := &MetricsRequest{}
			errChan := make(chan error, 1)

			go func() {
				errChan <- stream.RecvMsg(req)
			}()

			select {
			case err := <-errChan:
				cancel()
				if err == io.EOF {
					log.Printf("Client closed stream")
					return nil
				}
				if err != nil {
					if status.Code(err) == codes.Canceled {
						log.Printf("Client disconnected gracefully")
						return nil
					}
					log.Printf("Error receiving message: %v", err)
					return err
				}

				log.Printf("Received request with data length: %d", len(req.Data))

				// Unmarshal using easyproto
				metric, err := unmarshalMetricPoint(req.Data)
				if err != nil {
					log.Printf("Error unmarshaling metric: %v", err)
					// Send error response
					m := s.marshalerPool.Get().(*easyproto.Marshaler)
					mm := m.MessageMarshaler()
					mm.AppendBool(1, false)
					mm.AppendString(2, err.Error())
					resp := &MetricsResponse{Data: m.Marshal(nil)}
					m.Reset()
					s.marshalerPool.Put(m)
					if err := stream.SendMsg(resp); err != nil {
						log.Printf("Error sending error response: %v", err)
						return err
					}
					continue
				}

				// Process the metric
				log.Printf("Successfully unmarshaled metric: timestamp=%d, value=%f, isNull=%v", 
					metric.Timestamp, metric.Value, metric.IsNull)

				for _, label := range metric.Labels {
					log.Printf("Label: %s=%s", label.Name, label.Value)
				}

				if metric.Properties != nil {
					for key, value := range metric.Properties.Items {
						switch value.Type {
						case shared.ValueType_INT:
							log.Printf("Property %s: %d", key, *value.IntVal)
						case shared.ValueType_FLOAT:
							log.Printf("Property %s: %f", key, *value.FloatVal)
						case shared.ValueType_STRING:
							log.Printf("Property %s: %s", key, *value.StringVal)
						case shared.ValueType_LIST:
							log.Printf("Property %s: list with %d items", key, len(value.ListVal))
						}
					}
				}

				// Send success response
				m := s.marshalerPool.Get().(*easyproto.Marshaler)
				mm := m.MessageMarshaler()
				mm.AppendBool(1, true)
				mm.AppendString(2, "Processed successfully")
				resp := &MetricsResponse{Data: m.Marshal(nil)}
				m.Reset()
				s.marshalerPool.Put(m)
				if err := stream.SendMsg(resp); err != nil {
					if status.Code(err) == codes.Canceled {
						log.Printf("Client disconnected while sending response")
						return nil
					}
					log.Printf("Error sending success response: %v", err)
					return err
				}
				log.Printf("Successfully sent response")

			case <-readCtx.Done():
				cancel()
				if ctx.Err() != nil {
					// Parent context was canceled (client disconnected)
					log.Printf("Client disconnected during read")
					return nil
				}
				// Read timeout
				log.Printf("Read timeout, continuing...")
				continue
			}
		}
	}
}

func streamHandler(srv interface{}, stream grpc.ServerStream) error {
	s := srv.(*MetricsService)
	return s.ProcessMetrics(stream)
}

func unmarshalMetricPoint(data []byte) (*shared.MetricPoint, error) {
	mp := &shared.MetricPoint{
		Properties: &shared.KeyValue{
			Items: make(map[string]shared.Value),
		},
	}

	var fc easyproto.FieldContext
	remaining := data

	log.Printf("Starting to unmarshal metric point with data length: %d", len(data))

	for len(remaining) > 0 {
		var err error
		remaining, err = fc.NextField(remaining)
		if err != nil {
			log.Printf("Error getting next field: %v", err)
			return nil, fmt.Errorf("failed to get next field: %v", err)
		}

		log.Printf("Processing field number: %d", fc.FieldNum)

		switch fc.FieldNum {
		case 1: // Timestamp
			if v, ok := fc.Int64(); ok {
				mp.Timestamp = v
				log.Printf("Parsed timestamp: %d", v)
			} else {
				log.Printf("Failed to parse timestamp")
				return nil, fmt.Errorf("invalid timestamp field")
			}
		case 2: // Value
			if v, ok := fc.Double(); ok {
				mp.Value = v
				log.Printf("Parsed value: %f", v)
			} else {
				log.Printf("Failed to parse value")
				return nil, fmt.Errorf("invalid value field")
			}
		case 3: // IsNull
			if v, ok := fc.Bool(); ok {
				mp.IsNull = v
				log.Printf("Parsed isNull: %v", v)
			} else {
				log.Printf("Failed to parse isNull")
				return nil, fmt.Errorf("invalid isNull field")
			}
		case 4: // Labels
			labelData, ok := fc.MessageData()
			if !ok {
				log.Printf("Failed to get label message data")
				return nil, fmt.Errorf("invalid label message")
			}
			label, err := unmarshalLabel(labelData)
			if err != nil {
				log.Printf("Failed to unmarshal label: %v", err)
				return nil, fmt.Errorf("failed to unmarshal label: %v", err)
			}
			mp.Labels = append(mp.Labels, *label)
			log.Printf("Parsed label: %s=%s", label.Name, label.Value)
		case 5: // Properties
			propsData, ok := fc.MessageData()
			if !ok {
				log.Printf("Failed to get properties message data")
				return nil, fmt.Errorf("invalid properties message")
			}
			if err := unmarshalKeyValue(propsData, mp.Properties); err != nil {
				log.Printf("Failed to unmarshal properties: %v", err)
				return nil, fmt.Errorf("failed to unmarshal properties: %v", err)
			}
			log.Printf("Successfully parsed properties")
		}
	}

	return mp, nil
}

func unmarshalLabel(data []byte) (*shared.Label, error) {
	label := &shared.Label{}
	var fc easyproto.FieldContext
	remaining := data

	for len(remaining) > 0 {
		var err error
		remaining, err = fc.NextField(remaining)
		if err != nil {
			return nil, fmt.Errorf("failed to get next field in label: %v", err)
		}

		switch fc.FieldNum {
		case 1: // Name
			if v, ok := fc.String(); ok {
				label.Name = v
			} else {
				return nil, fmt.Errorf("invalid label name field")
			}
		case 2: // Value
			if v, ok := fc.String(); ok {
				label.Value = v
			} else {
				return nil, fmt.Errorf("invalid label value field")
			}
		}
	}

	return label, nil
}

func unmarshalKeyValue(data []byte, kv *shared.KeyValue) error {
	var fc easyproto.FieldContext
	remaining := data

	for len(remaining) > 0 {
		var err error
		remaining, err = fc.NextField(remaining)
		if err != nil {
			return fmt.Errorf("failed to get next field in keyvalue: %v", err)
		}

		switch fc.FieldNum {
		case 1: // Items
			itemData, ok := fc.MessageData()
			if !ok {
				return fmt.Errorf("invalid item message")
			}
			key, value, err := unmarshalMapEntry(itemData)
			if err != nil {
				return fmt.Errorf("failed to unmarshal map entry: %v", err)
			}
			kv.Items[key] = *value
		}
	}

	return nil
}

func unmarshalMapEntry(data []byte) (string, *shared.Value, error) {
	var key string
	var value *shared.Value
	var fc easyproto.FieldContext
	remaining := data

	for len(remaining) > 0 {
		var err error
		remaining, err = fc.NextField(remaining)
		if err != nil {
			return "", nil, fmt.Errorf("failed to get next field in map entry: %v", err)
		}

		switch fc.FieldNum {
		case 1: // Key
			if v, ok := fc.String(); ok {
				key = v
			} else {
				return "", nil, fmt.Errorf("invalid map key field")
			}
		case 2: // Value
			valueData, ok := fc.MessageData()
			if !ok {
				return "", nil, fmt.Errorf("invalid value message")
			}
			var err error
			value, err = unmarshalValue(valueData)
			if err != nil {
				return "", nil, fmt.Errorf("failed to unmarshal value: %v", err)
			}
		}
	}

	if key == "" {
		return "", nil, fmt.Errorf("missing map key")
	}
	if value == nil {
		return "", nil, fmt.Errorf("missing map value")
	}

	return key, value, nil
}

func unmarshalValue(data []byte) (*shared.Value, error) {
	value := &shared.Value{}
	var fc easyproto.FieldContext
	remaining := data

	for len(remaining) > 0 {
		var err error
		remaining, err = fc.NextField(remaining)
		if err != nil {
			return nil, fmt.Errorf("failed to get next field in value: %v", err)
		}

		switch fc.FieldNum {
		case 1: // Type
			if v, ok := fc.Int32(); ok {
				value.Type = shared.ValueType(v)
			} else {
				return nil, fmt.Errorf("invalid value type field")
			}
		case 2: // IntVal
			if v, ok := fc.Int64(); ok {
				value.IntVal = &v
			} else {
				return nil, fmt.Errorf("invalid int value field")
			}
		case 3: // FloatVal
			if v, ok := fc.Double(); ok {
				value.FloatVal = &v
			} else {
				return nil, fmt.Errorf("invalid float value field")
			}
		case 4: // StringVal
			if v, ok := fc.String(); ok {
				value.StringVal = &v
			} else {
				return nil, fmt.Errorf("invalid string value field")
			}
		case 5: // ListVal
			itemData, ok := fc.MessageData()
			if !ok {
				return nil, fmt.Errorf("invalid list item message")
			}
			item, err := unmarshalValue(itemData)
			if err != nil {
				return nil, fmt.Errorf("failed to unmarshal list item: %v", err)
			}
			value.ListVal = append(value.ListVal, *item)
		}
	}

	return value, nil
}

func main() {
	lis, err := net.Listen("tcp", shared.ServerAddress)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer(
		grpc.UnaryInterceptor(func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
			return handler(ctx, req)
		}),
		grpc.StreamInterceptor(func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
			return handler(srv, ss)
		}),
	)

	s.RegisterService(&grpc.ServiceDesc{
		ServiceName: shared.ServiceName,
		HandlerType: (*interface{})(nil),
		Methods:     []grpc.MethodDesc{},
		Streams: []grpc.StreamDesc{
			{
				StreamName:    shared.MethodName,
				Handler:      streamHandler,
				ServerStreams: true,
				ClientStreams: true,
			},
		},
	}, &MetricsService{
		marshalerPool: sync.Pool{
			New: func() interface{} {
				return &easyproto.Marshaler{}
			},
		},
	})

	log.Printf("Server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
