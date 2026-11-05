package cloudops

func init() {
	register(op{name: "project-codegraph-build"}, op{name: "session-codegraph-build"}, op{name: "project-codegraph", read: true}, op{name: "session-codegraph", read: true})
}
