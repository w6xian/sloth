package logger

type Logger interface {
	Output(calldepth int, s string) error
}

// LogLevel specifies the severity of a given log message
type LogLevel int

// Log levels
const (
	Debug LogLevel = iota
	Info
	Warning
	Error
	Max = iota - 1 // convenience - match highest log level
)

// String returns the string form for a given LogLevel
func (lvl LogLevel) String() string {
	switch lvl {
	case Info:
		return "INF"
	case Warning:
		return "WRN"
	case Error:
		return "ERR"
	}
	return "DBG"
}
