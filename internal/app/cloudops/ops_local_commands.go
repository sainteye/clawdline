package cloudops

func init() {
	register(
		// MARK: commands with a local capability

		op{name: "send",
			decode: func(b body) (plan, bool) {
				if !b.hasOneOf([]string{"type", "session", "text", "images"},
					[]string{"type", "session", "request", "text", "images"}) {
					return plan{}, false
				}
				p, ok := actionPlan(b, true)
				if !ok {
					return plan{}, false
				}
				text, textOK := b.str("text")
				raw, imagesOK := b["images"].([]any)
				if !textOK || !imagesOK {
					return plan{}, false
				}
				images := make([]string, 0, len(raw))
				for _, item := range raw {
					url, ok := item.(string)
					if !ok {
						return plan{}, false
					}
					images = append(images, url)
				}
				p.text, p.images = text, images
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/sessions/" + segment(p.target) + "/send",
					Body: jsonBody(map[string]any{"text": p.text, "images": p.images})}
			}},

		op{name: "answer", decode: decodeAnswer("answer"), route: routeAnswer, guard: answerNamesItsQuestion},
		// The Swift bridge takes `answer` and `key` as one case, and the
		// hosted console still sends either depending on how old the tab is.
		op{name: "key", decode: decodeAnswer("key"), route: routeAnswer, guard: answerNamesItsQuestion},

		op{name: "end",
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "accept_loss",
					"expected_closeability_version") {
					return plan{}, false
				}
				p, ok := actionPlan(b, true)
				if !ok || p.request == "" {
					return plan{}, false
				}
				acceptLoss, acceptOK := b.boolean("accept_loss")
				closeability, closeOK := b.str("expected_closeability_version")
				if !acceptOK || !closeOK {
					return plan{}, false
				}
				p.acceptLoss, p.closeability = acceptLoss, closeability
				return p, true
			},
			route: func(p plan) LocalRequest {
				// This daemon spells it `close`; the route compares the optional
				// version against its current Session reading.
				out := map[string]any{"force": p.acceptLoss}
				if p.closeability != "" {
					out["expected_closeability_version"] = p.closeability
				}
				return LocalRequest{Method: "POST",
					Path: "/v1/sessions/" + segment(p.target) + "/close", Body: jsonBody(out)}
			}},

		op{name: "focus",
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request") {
					return plan{}, false
				}
				p, ok := actionPlan(b, true)
				if !ok || p.request == "" {
					return plan{}, false
				}
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/sessions/" + segment(p.target) + "/focus"}
			}},

		// Stopping the turn a session is working on: the one Escape its own
		// working line asks for. Not a Swift word — that app had no stop — and
		// not `key`, whose allowlist is a menu's answers and which this bridge
		// refuses unless the answer names its question; a stop has no question.
		op{name: "interrupt",
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request") {
					return plan{}, false
				}
				p, ok := actionPlan(b, true)
				if !ok || p.request == "" {
					return plan{}, false
				}
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/sessions/" + segment(p.target) + "/interrupt", Body: []byte("{}")}
			}},

		op{name: "smart-title",
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request") {
					return plan{}, false
				}
				p, ok := actionPlan(b, true)
				if !ok || p.request == "" {
					return plan{}, false
				}
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/sessions/" + segment(p.target) + "/smart-title", Body: []byte("{}")}
			}},

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
		op{name: "restorable-sessions", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request") {
					return plan{}, false
				}
				return machinePlan(b)
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/sessions/restorable"}
			}},

		op{name: "restore-sessions",
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "conversations") {
					return plan{}, false
				}
				p, ok := actionPlan(b, false)
				if !ok || p.request == "" {
					return plan{}, false
				}
				ids, ok := conversationList(b["conversations"])
				if !ok || len(ids) == 0 {
					return plan{}, false
				}
				p.conversations = ids
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/sessions/restorable/restore",
					Body: jsonBody(map[string]any{"conversations": p.conversations})}
			}},

		op{name: "dismiss-restorable",
			decode: func(b body) (plan, bool) {
				if !b.hasOneOf([]string{"type", "session", "request"},
					[]string{"type", "session", "request", "conversations"}) {
					return plan{}, false
				}
				p, ok := actionPlan(b, false)
				if !ok || p.request == "" {
					return plan{}, false
				}
				raw, named := b["conversations"]
				if !named {
					p.allConversations = true
					return p, true
				}
				ids, ok := conversationList(raw)
				if !ok {
					return plan{}, false
				}
				p.conversations = ids
				return p, true
			},
			route: func(p plan) LocalRequest {
				if p.allConversations {
					return LocalRequest{Method: "POST", Path: "/v1/sessions/restorable/dismiss", Body: []byte("{}")}
				}
				return LocalRequest{Method: "POST", Path: "/v1/sessions/restorable/dismiss",
					Body: jsonBody(map[string]any{"conversations": p.conversations})}
			}},

		// The Sessions a person archived (docs/session-archive.md). The
		// archive names the session it closes, so its answer rides that
		// session's channel as `end`'s does; `force` is `end`'s
		// `accept_loss`. The list is a machine read, and the restore a
		// machine command that carries the viewer's request as its
		// Idempotency-Key, as `restore-sessions` does.
		op{name: "archive-session",
			decode: func(b body) (plan, bool) {
				if !b.hasOneOf([]string{"type", "session", "request"},
					[]string{"type", "session", "request", "force"},
					[]string{"type", "session", "request", "expected_closeability_version"},
					[]string{"type", "session", "request", "force", "expected_closeability_version"}) {
					return plan{}, false
				}
				p, ok := actionPlan(b, true)
				if !ok || p.request == "" {
					return plan{}, false
				}
				if _, named := b["force"]; named {
					force, forceOK := b.boolean("force")
					if !forceOK {
						return plan{}, false
					}
					p.acceptLoss = force
				}
				if _, named := b["expected_closeability_version"]; named {
					version, ok := b.str("expected_closeability_version")
					if !ok {
						return plan{}, false
					}
					p.closeability = version
				}
				return p, true
			},
			route: func(p plan) LocalRequest {
				out := map[string]any{"force": p.acceptLoss}
				if p.closeability != "" {
					out["expected_closeability_version"] = p.closeability
				}
				return LocalRequest{Method: "POST", Path: "/v1/sessions/" + segment(p.target) + "/archive",
					Body: jsonBody(out)}
			}},

		op{name: "archived-sessions", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request") {
					return plan{}, false
				}
				return machinePlan(b)
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/sessions/archived"}
			}},

		op{name: "restore-archived",
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "conversations") {
					return plan{}, false
				}
				p, ok := actionPlan(b, false)
				if !ok || p.request == "" {
					return plan{}, false
				}
				ids, ok := conversationList(b["conversations"])
				if !ok || len(ids) == 0 {
					return plan{}, false
				}
				p.conversations = ids
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/sessions/archived/restore",
					Body: jsonBody(map[string]any{"conversations": p.conversations})}
			}},

		op{name: "voice",
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "audio", "rate") {
					return plan{}, false
				}
				p, ok := actionPlan(b, false)
				if !ok || p.request == "" {
					return plan{}, false
				}
				audio, audioOK := b.nonEmpty("audio")
				rate, rateOK := b.integer("rate")
				// Checked rather than resampled, as the route checks it: a body
				// that names 48000 has not made a small mistake.
				if !audioOK || !rateOK || rate != voiceRate {
					return plan{}, false
				}
				p.audio, p.rate = audio, rate
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/voice",
					Body: jsonBody(map[string]any{"audio": p.audio, "rate": p.rate})}
			}},

		op{name: "intents",
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "text") {
					return plan{}, false
				}
				p, ok := actionPlan(b, false)
				if !ok || p.request == "" {
					return plan{}, false
				}
				text, ok := b.str("text")
				if !ok || len([]byte(text)) > intentTextLimit {
					return plan{}, false
				}
				p.text = text
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/intents",
					Body: jsonBody(map[string]any{"text": p.text})}
			}},

		op{name: "schedule-create",
			decode: decodeScheduleWrite(false),
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/orchestrator/schedules",
					Body: p.document, Header: asDevice()}
			}},

		op{name: "schedule-update",
			decode: decodeScheduleWrite(true),
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "PATCH",
					Path: "/v1/orchestrator/schedules/" + segment(p.id),
					Body: p.document, Header: asDevice()}
			}},

		op{name: "schedule-delete",
			decode: decodeNamedSchedule,
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "DELETE",
					Path: "/v1/orchestrator/schedules/" + segment(p.id), Header: asDevice()}
			}},

		op{name: "schedule-run",
			decode: decodeNamedSchedule,
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST",
					Path:   "/v1/orchestrator/schedules/" + segment(p.id) + "/run",
					Body:   []byte("{}"),
					Header: asDevice()}
			}},

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
		op{name: "snippet-create",
			decode: decodeSnippetWrite(false),
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/snippets",
					Body: p.document, Header: asDevice()}
			}},

		op{name: "snippet-update",
			decode: decodeSnippetWrite(true),
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "PATCH", Path: "/v1/snippets/" + segment(p.id),
					Body: p.document, Header: asDevice()}
			}},

		op{name: "snippet-delete",
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "id") {
					return plan{}, false
				}
				p, ok := actionPlan(b, false)
				if !ok || p.request == "" {
					return plan{}, false
				}
				id, ok := b.nonEmpty("id")
				if !ok {
					return plan{}, false
				}
				p.id = id
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "DELETE", Path: "/v1/snippets/" + segment(p.id),
					Header: asDevice()}
			}},

		// One group's complete order. `ordering` is the producer's name for
		// the body the local route reads under no name at all, which is the
		// one place these two spellings differ.
		op{name: "snippet-order",
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "ordering") {
					return plan{}, false
				}
				p, ok := actionPlan(b, false)
				if !ok || p.request == "" {
					return plan{}, false
				}
				document, ok := b.object("ordering", snippetMaximumBytes)
				if !ok {
					return plan{}, false
				}
				p.document = document
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/snippets/order",
					Body: p.document, Header: asDevice()}
			}},

		// The three words that change something about notifications, all
		// read-level: the Swift bridge lists exactly these three beside the
		// two diagnostics words in `readLevelCommandTypes`. The write switch
		// is about typing into somebody's session, and asking to be told when
		// one needs an answer is the reading half arriving by another road —
		// a phone paired read-only is the device this feature exists for.
		//
		// Each carries `X-Clawdline-Actor: device` for the reason the schedule
		// writes do, said for a different door: `/v1/push/unsubscribe` exempts
		// this machine's own token from the check that a subscription belongs
		// to the device removing it, because that exemption is how a script
		// cleans up after itself. A Cloud viewer reaches these routes in
		// process holding exactly that token, so without this header a person
		// on their phone would arrive wearing the script's exemption.
		op{name: "push-subscribe", readLevel: true,
			divergence: "every Cloud viewer is the same device here: an in-process Cloud " +
				"request carries this machine's own local credential, and the store keeps " +
				"one row per device, so a second Cloud browser asking to be notified takes " +
				"the first one's place — where two devices paired to this machine's own " +
				"network keep a row each. It needs the viewer's own identity to reach the " +
				"route, which no word on this wire carries. Measured in " +
				"internal/transport/http's `TestEveryCloudViewerIsTheSameDeviceHere`. " +
				"The web-app origin stored beside it was this daemon's `http://127.0.0.1` " +
				"for a while, which is what an iOS declarative notification resolved its " +
				"address against and is why a notification that arrived opened nothing; " +
				"it is now `cloud_app_origin`, carried to the route beside the credential",
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

		op{name: "push-unsubscribe", readLevel: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "id") {
					return plan{}, false
				}
				p, ok := actionPlan(b, false)
				if !ok || p.request == "" {
					return plan{}, false
				}
				id, ok := b.nonEmpty("id")
				if !ok {
					return plan{}, false
				}
				p.id = id
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/push/unsubscribe",
					Body:   jsonBody(map[string]any{"id": p.id}),
					Header: asDevice()}
			}},

		op{name: "push-test", readLevel: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "target") {
					return plan{}, false
				}
				p, ok := actionPlan(b, false)
				if !ok || p.request == "" {
					return plan{}, false
				}
				// An empty target is a test with nothing to tap back to,
				// which goes to the list: the key is required, its value may
				// be "". The route's field for it is `session_id`, and the
				// body leaves it out entirely rather than sending an empty
				// one, as the Swift bridge does.
				target, ok := b.str("target")
				if !ok {
					return plan{}, false
				}
				p.target = target
				return p, true
			},
			route: func(p plan) LocalRequest {
				sent := map[string]any{}
				if p.target != "" {
					sent["session_id"] = p.target
				}
				return LocalRequest{Method: "POST", Path: "/v1/push/test",
					Body: jsonBody(sent), Header: asDevice()}
			}},
	)
}
