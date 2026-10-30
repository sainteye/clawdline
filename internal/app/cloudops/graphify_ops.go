package cloudops

func init() {
	register(op{name: "project-graphify", read: true}, op{name: "session-graphify", read: true})
}
