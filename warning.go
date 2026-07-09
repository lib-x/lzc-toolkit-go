package lpkgo

type Severity string

const (
	SeverityInfo    Severity = "INFO"
	SeverityWarning Severity = "WARNING"
	SeverityError   Severity = "ERROR"
)

type Warning struct {
	Code     string
	Severity Severity
	Path     string
	Message  string
}
