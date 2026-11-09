package pluginapi

// URLOpenHost is the optional Host part behind gvim.url.open: it validates the URL,
// asks the user to confirm, and starts the platform opener off the update loop. A host
// without it answers CodeUnsupported.
type URLOpenHost interface {
	// OpenURL validates raw and, when it is a safe http/https URL, asks the user to
	// confirm opening it. It returns once the confirmation is queued; the opener runs
	// asynchronously. An unsafe URL is refused with CodeInvalid; the user may refuse
	// the confirmation, in which case nothing is opened.
	OpenURL(raw string) error
}

// MaxOpenURLLen is the most bytes gvim.url.open accepts.
const MaxOpenURLLen = 2048
