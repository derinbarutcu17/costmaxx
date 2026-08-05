package artifacts

type Reducer interface {
	Name() string
	Version() string
	CanHandle(category string, command string, exitCode int, size int64) float64
	Reduce(input string, metadata ReducerMetadata) (*ReductionRecord, error)
}

type ReducerMetadata struct {
	Command  string
	ExitCode int
	Category string
	ToolName string
	Size     int64
}
