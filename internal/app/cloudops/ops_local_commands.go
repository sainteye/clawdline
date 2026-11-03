package cloudops

func init() {
	register(
		// MARK: commands with a local capability

		op{name: "direct-local-network-enable"},

		op{name: "send"},
		op{name: "native-terminal-input"},

		op{name: "answer", guard: answerNamesItsQuestion},
		// The Swift bridge takes `answer` and `key` as one case, and the
		// hosted console still sends either depending on how old the tab is.
		op{name: "key", guard: answerNamesItsQuestion},

		op{name: "end"},

		op{name: "focus"},

		// Stopping the turn a session is working on: the one Escape its own
		// working line asks for. Not a Swift word — that app had no stop — and
		// not `key`, whose allowlist is a menu's answers and which this bridge
		// refuses unless the answer names its question; a stop has no question.
		op{name: "interrupt"},

		op{name: "smart-title"},

		op{name: "title"},

		op{name: "start",
			decode: func(b body) (plan, bool) {
				// `persona` is optional: a page from before personas sends the
				// six keys it always sent, and one that chose a persona sends a
				// seventh (docs/personas.md).
				if !b.hasOneOf([]string{"type", "session", "request", "place", "assistant", "model"},
					[]string{"type", "session", "request", "place", "assistant", "model", "persona"}) {
					return plan{}, false
				}
				p, ok := actionPlan(b, false)
				if !ok || p.request == "" {
					return plan{}, false
				}
				place, placeOK := b.nonEmpty("place")
				assistant, assistantOK := b.str("assistant")
				model, modelOK := b.str("model")
				persona, personaOK := personaName(b)
				if !placeOK || !assistantOK || !modelOK || !personaOK {
					return plan{}, false
				}
				p.place, p.assistant, p.model, p.persona = place, assistant, model, persona
				return p, true
			},
			route: func(p plan) LocalRequest {
				route := "/v1/places/" + segment(p.place) + "/start"
				assistant := p.assistant
				if assistant == "" {
					assistant = "claude"
				}
				// `/as/<persona>` is recognised only after a named assistant,
				// so a persona always spells the assistant out.
				if p.assistant != "" || p.model != "" || p.persona != "" {
					route += "/" + segment(assistant)
				}
				if p.model != "" {
					route += "/" + segment(p.model)
				}
				if p.persona != "" {
					route += "/as/" + segment(p.persona)
				}
				return LocalRequest{Method: "POST", Path: route, Body: []byte("{}")}
			}},

		op{name: "resume",
			decode: func(b body) (plan, bool) {
				if !b.hasOneOf([]string{"type", "session", "request", "place", "past", "assistant"},
					[]string{"type", "session", "request", "place", "past", "assistant", "persona"}) {
					return plan{}, false
				}
				p, ok := actionPlan(b, false)
				if !ok || p.request == "" {
					return plan{}, false
				}
				place, placeOK := b.nonEmpty("place")
				past, pastOK := b.nonEmpty("past")
				assistant, assistantOK := b.str("assistant")
				persona, personaOK := personaName(b)
				if !placeOK || !pastOK || !assistantOK || !personaOK {
					return plan{}, false
				}
				p.place, p.past, p.assistant, p.persona = place, past, assistant, persona
				return p, true
			},
			route: func(p plan) LocalRequest {
				route := "/v1/places/" + segment(p.place) + "/resume/"
				assistant := p.assistant
				// The route reads `/as/<persona>` only on a resume that names
				// its assistant, so a persona spells out the default one.
				if assistant == "" && p.persona != "" {
					assistant = "claude"
				}
				if assistant != "" {
					route += segment(assistant) + "/"
				}
				route += segment(p.past)
				if p.persona != "" {
					route += "/as/" + segment(p.persona)
				}
				return LocalRequest{Method: "POST", Path: route, Body: []byte("{}")}
			}},

		// The sessions a reboot took away (docs/session-restore.md). The
		// read is the list; the two commands carry the viewer's request as
		// their Idempotency-Key, as `resume` does, because a restore is one
		// resume per conversation and a retried envelope must not open a
		// second tab.
		op{name: "restorable-sessions", read: true},

		op{name: "restore-sessions"},

		op{name: "dismiss-restorable"},

		// The Sessions a person archived (docs/session-archive.md). The
		// archive names the session it closes, so its answer rides that
		// session's channel as `end`'s does; `force` is `end`'s
		// `accept_loss`. The list is a machine read, and the restore a
		// machine command that carries the viewer's request as its
		// Idempotency-Key, as `restore-sessions` does.
		op{name: "archive-session"},

		op{name: "archived-sessions", read: true},

		op{name: "restore-archived"},

		op{name: "forget-archived"},

		op{name: "voice"},

		op{name: "intents"},

		op{name: "schedule-create"},

		op{name: "schedule-update"},

		op{name: "schedule-delete"},

		op{name: "schedule-run"},

		// The old hosted console's two-phase webhook creation flow. Cloud creates
		// the capability under the browser session, then this encrypted command
		// makes the selected Mac persist the schedule binding and activate it with
		// its machine credential. The trigger URL is deliberately absent here.
		op{name: "schedule-webhook-bind-v1",
			decode: decodeScheduleWebhookBind,
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/orchestrator/schedule-webhooks/bind",
					Body: p.document, Header: asDevice()}
			}},

		// The four snippet writes. The key sets are the producer's, word for
		// word (`net/cloud-client.js`: `createSnippet`, `updateSnippet`,
		// `deleteSnippet`, `orderSnippets`), and each one hands its
		// sub-document straight to the local route that owns its shape — this
		// bridge knows what a request looks like, not what a snippet looks
		// like.
		//
		// They carry `X-Clawdline-Actor: device` for the reason the schedule
		// writes do, said for this door: `snippetWrite` asks which door the
		// caller came in by, and the answer decides the name the write is
		// filed under in the receipt table (`personPrincipal`). Without the
		// header an in-process Cloud request is this machine's own hand —
		// `local` — so a person's press from their phone and a script's write
		// on this machine would share one receipt namespace. It opens nothing:
		// the route needs a device that may send, which it needed anyway.
		// Within that name each Cloud viewer files under its own
		// (`viewerPrincipal`), so two phones naming the same request id are
		// two requests.
		op{name: "snippet-create"},

		op{name: "snippet-update"},

		op{name: "snippet-delete"},

		// One group's complete order. `ordering` is the producer's name for
		// the body the local route reads under no name at all, which is the
		// one place these two spellings differ.
		op{name: "snippet-order"},

		// The three words that change something about notifications, all
		// read-level: the Swift bridge lists exactly these three beside the
		// two diagnostics words in `readLevelCommandTypes`. The write switch
		// is about typing into somebody's session, and asking to be told when
		// one needs an answer is the reading half arriving by another road,
		// so a phone keeps its notifications with the switch off.
		//
		// Each carries `X-Clawdline-Actor: device` for the reason the schedule
		// writes do, said for a different door: `/v1/push/unsubscribe` exempts
		// this machine's own token from the check that a subscription belongs
		// to the device removing it, because that exemption is how a script
		// cleans up after itself. A Cloud viewer reaches these routes in
		// process holding exactly that token, so without this header a person
		// on their phone would arrive wearing the script's exemption.
		//
		// Who owns a row is the viewer, not the credential. The bridge carries
		// the envelope's verified sender to the route (`LocalRequest.Viewer`,
		// read there as `cloud.CloudViewerOf`), and the route owns the row as
		// `cloud:<viewer>`, so two phones keep a row each and neither can
		// remove or test the other's, nor this machine's console's.
		op{name: "push-subscribe", readLevel: true,
			divergence: "a subscription a Cloud viewer made before each viewer became its " +
				"own device here (2026-10-19) is still owned by this machine's own console: " +
				"it keeps being notified, but that viewer cannot remove or test it until it " +
				"subscribes again, which replaces the row by its endpoint. Every subscription " +
				"made since belongs to the viewer that made it, measured in " +
				"internal/transport/http's `TestEachCloudViewerKeepsItsOwnSubscription`. " +
				"The web-app origin stored beside it is `cloud_app_origin`, carried to the " +
				"route beside the credential",
			decode: func(b body) (plan, bool) {
				// `cloud_subscription_id` is present when the browser
				// subscribed with Clawdline Cloud's VAPID key rather than this
				// machine's: the row is then sealed here and forwarded
				// through Cloud (docs/push.md, "Cloud-sent push").
				if !b.hasOneOf([]string{"type", "session", "request", "subscription"},
					[]string{"type", "session", "request", "subscription", "cloud_subscription_id"}) {
					return plan{}, false
				}
				p, ok := actionPlan(b, false)
				if !ok || p.request == "" {
					return plan{}, false
				}
				// The browser's own object, carried as the bytes it will
				// travel as. What counts as a usable subscription is the
				// route's question (`push.FromBrowser`), and it is asked
				// there so that an endpoint this machine will POST to is
				// checked in exactly one place — the cloud id with it,
				// which rides beside the object's own keys.
				subscription, ok := b["subscription"].(map[string]any)
				if !ok {
					return plan{}, false
				}
				if cloudID, present := b["cloud_subscription_id"]; present {
					if _, clash := subscription["cloud_subscription_id"]; clash {
						return plan{}, false
					}
					merged := make(map[string]any, len(subscription)+1)
					for key, value := range subscription {
						merged[key] = value
					}
					merged["cloud_subscription_id"] = cloudID
					b = body{"subscription": merged}
				}
				document, ok := b.object("subscription", pushBodyMaximumBytes)
				if !ok {
					return plan{}, false
				}
				p.document = document
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/push/subscribe",
					Body: p.document, Header: asDevice()}
			}},

		op{name: "push-unsubscribe", readLevel: true},

		op{name: "push-test", readLevel: true},
	)
}
