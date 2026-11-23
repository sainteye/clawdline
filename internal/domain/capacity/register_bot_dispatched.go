package capacity

// Mirrors DispatchedStripRows in the Bot chat's dispatched-work strip. Further
// rows are counted in the strip and remain listed in the Sessions view.
const MaximumBotDispatchedStripRows = 5

const (
	BotsDispatchedStripRows        = "bots.dispatched_strip_rows"
	BotsDispatchedDeliveredSeconds = "bots.dispatched_delivered_seconds"
)
