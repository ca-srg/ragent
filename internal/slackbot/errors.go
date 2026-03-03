package slackbot

type ErrorReporter interface {
	Report(err error, context map[string]string)
}

type noopReporter struct{}

func (n *noopReporter) Report(err error, context map[string]string) {}
