// Package redsample is the register guard's control: it declares bounded
// things nobody registered, and TestTheScanFindsAnUnregisteredBound fails if
// the scan does not find every one of them. It is under testdata, so the
// build never sees it.
package redsample

// MaximumWidgets is a bound: found.
const MaximumWidgets = 10

// widgetLimit is a bound: found.
var widgetLimit = 20

// maximal is not a bound by the naming rule: not found.
const maximal = true

type Pump struct{ in chan int }

func (p *Pump) start(depth int) {
	// A constant bound inside a function: found.
	const maxRetries = 3
	// A local variable named limit is a parameter of one call: not found.
	var limit = depth
	_ = limit
	// A buffered channel: found.
	p.in = make(chan int, depth)
	// A channel of one is a latch: not found.
	_ = make(chan struct{}, 1)
}
