package shared

type MetricPoint struct {
	Timestamp  int64
	Value      float64
	IsNull     bool
	Labels     []Label
	Properties *KeyValue
}

type Label struct {
	Name  string
	Value string
}

type KeyValue struct {
	Items map[string]Value
}

type Value struct {
	Type      ValueType
	IntVal    *int64
	FloatVal  *float64
	StringVal *string
	ListVal   []Value
}

type ValueType int32

const (
	ValueType_UNSPECIFIED ValueType = 0
	ValueType_INT         ValueType = 1
	ValueType_FLOAT       ValueType = 2
	ValueType_STRING      ValueType = 3
	ValueType_LIST        ValueType = 4
)

const (
	ServiceName    = "metrics.MetricsService"
	MethodName     = "ProcessMetrics"
	ServerAddress  = ":50051"
	FullMethodPath = "/" + ServiceName + "/" + MethodName
)
