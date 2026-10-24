package cloudops

func init() {
	register(
		// MARK: the work system, and what a person is looking at while it runs
		//
		// Participation reads remain available while the v1 board and Backlog
		// reads are retired. The v2 Board uses its own Cloud words below.

		op{name: "work.proposals", read: true},

		op{name: "work.decisions", read: true},
		op{name: "work.decision", read: true},
		op{name: "work.decision-answer"},

		op{name: "work.digests", read: true},

		// Work-system v2 is the console's current Board. These are separate
		// words because a machine descriptor must be able to say exactly which
		// reads and person-only writes it implements. Every write is stamped as
		// a paired device: a Cloud viewer is a person using this machine, not an
		// Agent or the machine's orchestrator.
		op{name: "work.v2.item", read: true},
		op{name: "work.v2.gate-export", read: true},

		op{name: "work.v2.items", read: true},

		op{name: "work.v2.search", read: true},

		op{name: "work.v2.proposals", read: true},

		op{name: "work.v2.session-todos", read: true},
		op{name: "work.v2.human-interventions", read: true},

		// `size: "thumb"` is the picture a Board card or a to-do row draws,
		// and the only size there is besides the whole one. A page built
		// before it sends no `size` and is answered the full image, as it
		// always was.
		op{name: "work.v2.image", read: true, shape: shapeImage},

		op{name: "work.v2.create"},

		op{name: "work.v2.edit"},
		op{name: "work.v2.gate-decision"},
		op{name: "work.v2.gate-purge"},

		op{name: "work.v2.assign"},
		op{name: "work.v2.convert"},

		op{name: "work.v2.persona-suggestion"},

		op{name: "work.v2.remind"},

		op{name: "work.v2.cancel"},

		op{name: "work.v2.complete"},

		// The person's read receipt for a deploying or done item; the local
		// route judges `item.phase` and that a person, not an Agent, saw it.
		op{name: "work.v2.seen"},

		op{name: "work.v2.image-create"},

		op{name: "work.v2.image-delete"},

		op{name: "work.v2.proposal-resolve"},

		op{name: "work.v2.todo-create"},

		op{name: "work.v2.todo-image-create"},

		op{name: "work.v2.todo-action"},
		op{name: "work.v2.human-intervention-action"},

		// The Projects page's catalog and worktree lifecycle. `places` above is
		// carried separately.
		op{name: "project-folders", read: true},
		op{name: "project-add"},
		op{name: "project-hide"},
		op{name: "projects", read: true},
		op{name: "project-file-list", read: true},
		op{name: "project-file-read", read: true},
		op{name: "project-tree-list", read: true},
		op{name: "project-tree-read", read: true},
		// Unify (docs/project-files.md): the plan is a read a paired device
		// may make with remote writes off; apply is a write and needs the gate.
		op{name: "project-antigravity-consent-status", read: true},
		op{name: "project-antigravity-consent-change"},
		op{name: "project-unify-plan", read: true},
		op{name: "project-unify-apply",
			partial: stoppedUnify},
		op{name: "project-file-save"},

		// Project settings sync (docs/project-sync.md): a source offers, a
		// mirror applies. The viewer is the courier; neither machine reaches
		// the other, and the relay carries only ciphertext.
		op{name: "project-manifest", read: true},
		op{name: "project-entry", read: true},
		op{name: "project-mirror", read: true},
		op{name: "project-mirror-apply"},
		op{name: "project-mirror-detach"},

		op{name: "project-worktree-lifecycle", read: true},

		op{name: "project-worktree-lifecycle-refresh"},
		op{name: "project-worktree-remove"},

		// The capacity block on the Settings page (docs/limits.md §4.5 "the
		// screen"): every row of the register and the dead letters. A capacity
		// push names that block, and the person reads it on the phone that got
		// the push, so the one route crosses whole. Machine-wide and without
		// a parameter, as the local route is; a read, since the route writes
		// nothing.
		op{name: "capacity", read: true},
		op{name: "provider-quotas", read: true},

		// The machine-wide launch defaults shown on Settings. This is deliberately
		// narrower than /v1/settings: a paired phone receives and changes only
		// these values, never the machine's other configuration.
		op{name: "default-models", read: true},

		op{name: "default-models-update"},

		// A paired phone may edit only notification timing rules, through the
		// same narrow route exposed by the local console.
		op{name: "notification-rules", read: true},
		op{name: "notification-rules-update"},

		// The two Work v2 gate defaults shown by every Settings page. Like the
		// default-model route above, this is deliberately narrower than
		// /v1/settings: a paired device can neither read nor write another
		// machine setting through this word.
		op{name: "work-gate-settings", read: true},
		op{name: "workflow-settings", read: true},
		op{name: "workflow-settings-update"},

		op{name: "notification-settings", read: true},

		op{name: "notification-settings-update"},

		op{name: "work-gate-settings-update"},

		// The dashboard behind the session counts: this machine's CPU and
		// memory and each session's share. Machine-wide and parameterless, as
		// the local route is; a read, since the route writes nothing.
		op{name: "machine-usage", read: true},

		// Whether this machine trails the cloud's latest build
		// (docs/updates.md): machine-wide and parameterless, as the local
		// route is; a read, since the route answers from the last background
		// check and writes nothing.
		op{name: "update", read: true},
		// A foreground return requires a fresh signed manifest. A distinct word
		// keeps older machines' update read wire shape unchanged.
		op{name: "update-fresh", read: true},

		// The Settings page's 「立即更新」 (docs/updates.md): install the newest
		// release of this machine's channel. A command, so it needs the
		// machine's Cloud-command switch like any other write; nothing but the
		// request crosses, so a phone can neither name a version nor force one.
		op{name: "update-apply"},

		op{name: "agent", read: true},

		op{name: "shell", read: true}, // The Shell panel's read (internal/transport/http/shells.go). The
		// window is the local route's `sessions.shell_output_bytes`, so
		// the two bounds are one.

	)
}
