import AppKit
import Carbon.HIToolbox
import Foundation
import SQLite3

// MARK: - One sentence, turned into a draft of a session

/// Three projects with ids of their own, so a test can tell "the second one" from "not that one"
/// without depending on what this Mac has been worked in.
let plannerPlaces = [
    StartPoints.Place(id: StartPoints.id(for: "/Users/me/code/clawdline"),
                      path: "/Users/me/code/clawdline", label: "clawdline",
                      at: Date(timeIntervalSince1970: 3)),
    StartPoints.Place(id: StartPoints.id(for: "/Users/me/code/notebook"),
                      path: "/Users/me/code/notebook", label: "notebook",
                      at: Date(timeIntervalSince1970: 2)),
    StartPoints.Place(id: StartPoints.id(for: "/Users/me/code/cairn"),
                      path: "/Users/me/code/cairn", label: "cairn",
                      at: Date(timeIntervalSince1970: 1)),
]

func runPlannerTests() {
group("the planner is shown a numbered list and never a way to write a path") {
    let prompt = Planner.prompt(places: plannerPlaces, assistants: [.claude, .codex])

    // The numbering is the whole of the safety argument: the model answers with an index and
    // Swift maps it back, so a list that numbered from zero would shift every choice by one.
    check("the list starts at one", prompt.contains("1. clawdline — /Users/me/code/clawdline"))
    check("and counts up", prompt.contains("2. notebook — /Users/me/code/notebook"))
    check("to the last one", prompt.contains("3. cairn — /Users/me/code/cairn"))
    check("nothing is numbered zero", !prompt.contains("0. "))
    check("the model is told what zero means instead", prompt.contains("or 0 when none"))
    check("and told in as many words not to write a path", prompt.contains("Never write a path"))

    // Only the assistants this Mac actually has. A draft naming one that is not installed opens
    // a tab that says "command not found" — the same reason `/v1/places` sends the list.
    let alone = Planner.prompt(places: plannerPlaces, assistants: [.claude])
    check("the assistants offered are the ones passed in", alone.contains("you may pick: claude."))
    check("and codex is not one of them when it is not there", !alone.contains("codex"))

    // How hard the work is, which is the second judgement made here about a session nobody will
    // watch start. The schema refuses a name that is not one of the three and can say nothing
    // about which of the three is right, so the prompt names all three by what they are for.
    check("the three sizes are offered by name", prompt.contains("out of haiku, sonnet, opus"))
    check("the smallest is for work where being wrong is obvious",
          prompt.contains("\"haiku\" is for mechanical single-source work"))
    check("the middle one is what to write when it is not clear which this is",
          prompt.contains("the answer whenever you are unsure which of the three this is"))
    check("and the largest is for something somebody acts on unchecked",
          prompt.contains("\"opus\" is for work somebody will act on without checking it first"))
    check("a Mac with one assistant is still asked how big the session should be",
          alone.contains("out of haiku, sonnet, opus"))

    // Codex says how hard a job is as reasoning effort, set where work is handed to it. A draft
    // naming a model for it would be a second answer to the same question — and the rule is only
    // worth prompt space on a Mac that has Codex to draft for.
    check("codex is told to name none of them",
          prompt.contains("Leave this empty when the assistant is codex"))
    check("and the rule is left out where there is no codex to apply it to",
          !alone.contains("Leave this empty"))

    // The transcript is speech, and the measured behaviour this buys is a misheard project name
    // matching the right project rather than being read as a new one.
    check("the model is told the sentence came from speech",
          prompt.contains("speech-to-text"))
    check("and that a near miss is not a new project",
          prompt.contains("is that project, not a new one"))

    // A Mac with nowhere to start a session still has to produce a prompt, and one that listed
    // nothing at all would leave the model to invent a project rather than answer zero.
    let nowhere = Planner.prompt(places: [], assistants: [.claude])
    check("an empty list says it is empty", nowhere.contains("(none"))
    check("and still says what zero means", nowhere.contains("or 0 when none"))

    // The threshold is a number in one place, and the prompt is one of the things that has to
    // agree with it — a model told 0.7 and read at 0.5 is a model whose guesses are taken as
    // answers.
    check("the confidence the prompt names is the one the code reads",
          prompt.contains("Below \(Planner.sure) when you are guessing"))

    // A sentence can now ask for work that repeats, which is a second thing to be told and a
    // second thing to be told not to invent.
    check("the two things a sentence can ask for are named",
          prompt.contains("\"schedule\" when the sentence asks for work to happen at a time"))
    check("and the tie is broken towards the cheaper mistake",
          prompt.contains("When it could be either it is \"session\""))
    check("the time is asked for in the spelling the schedule file takes",
          prompt.contains("as HH:MM on a 24-hour clock"))
    check("a time with no day named is every day, rather than a guess at one",
          prompt.contains("A time with nothing said about which day is [\"daily\"]"))
    check("and the weekday example is the one the plan named",
          prompt.contains("[\"mon\",\"tue\",\"wed\",\"thu\",\"fri\"]"))

    // The one this system cannot express. A model left to itself turns "tomorrow at three" into
    // a weekly repeat, which is work nobody asked for arriving every week until they find it.
    check("the model is told there are no one-off schedules here",
          prompt.contains("There are no one-off schedules on this Mac"))
    check("and told what to do with a sentence that asks for one",
          prompt.contains("\"tomorrow at three\"")
            && prompt.contains("confidence below \(Planner.sure) and a question"))

    // The schema is built from the same list the prompt is and read by the primary engine only,
    // so it is checked here rather than assumed: a fence-post in the interpolation would produce
    // a `--json-schema` argument the CLI refuses, and the visible symptom is every draft falling
    // through to the fallback engine.
    check("the schema is still an object after the sizes were written into it",
          (try? JSONSerialization.jsonObject(with: Data(Planner.schema.utf8))) != nil)
    check("it offers the three sizes and the empty string, and nothing else",
          Planner.schema.contains(
            "\"model\":{\"type\":\"string\",\"enum\":[\"\",\"haiku\",\"sonnet\",\"opus\"]}"))
    check("and asks for the field rather than letting it be left out",
          Planner.schema.contains("\"assistant\",\"model\",\"instructions\""))
}

group("a time and a set of days survive the trip out of a model, or do not") {
    // Padded rather than refused: `9:00` and `09:00` are the same time and only the second is a
    // schedule file's `when.at`, so a dropped zero must not empty the form.
    expect("an unpadded hour is padded", Planner.time(inAnswer: "9:00"), "09:00")
    expect("an already-padded one is left alone", Planner.time(inAnswer: "09:30"), "09:30")
    expect("midnight is a time", Planner.time(inAnswer: "0:0"), "00:00")
    expect("and the last minute of the day", Planner.time(inAnswer: "23:59"), "23:59")
    expect("surrounding whitespace is not part of it", Planner.time(inAnswer: " 9:05 "), "09:05")

    // Everything that is not a time is no time at all, and it has to be decided here: carried
    // through, `25:00` becomes a file the parser refuses one screen later, with a worse sentence
    // attached to it than the form could have shown.
    expect("an hour nobody has is nothing", Planner.time(inAnswer: "25:00"), "")
    expect("and a minute nobody has", Planner.time(inAnswer: "09:60"), "")
    expect("a time with no colon is nothing", Planner.time(inAnswer: "0930"), "")
    expect("nor is a signed one", Planner.time(inAnswer: "+9:00"), "")
    expect("nor one with seconds on it", Planner.time(inAnswer: "09:30:00"), "")
    expect("nor digits that are not ASCII", Planner.time(inAnswer: "٠٩:٣٠"), "")
    expect("an empty string says the sentence had no time", Planner.time(inAnswer: ""), "")
    expect("and so does a missing field", Planner.time(inAnswer: nil), "")
    expect("a number where the time goes is not one", Planner.time(inAnswer: 930), "")

    // The schema asks for a list and spells every day `["daily"]`; the fallback engine has no
    // schema and writes the bare word about as often.
    expect("the schema's spelling of every day", Planner.weekdays(inAnswer: ["daily"]),
           Planner.Draft.Days.daily)
    expect("and the bare word", Planner.weekdays(inAnswer: "daily"), Planner.Draft.Days.daily)
    expect("long weekday names are the same days as short ones",
           Planner.weekdays(inAnswer: ["Monday", "wednesday"]),
           Planner.Draft.Days.named(["mon", "wed"]))
    expect("they come back in week order however they were written",
           Planner.weekdays(inAnswer: ["fri", "mon", "sun"]),
           Planner.Draft.Days.named(["sun", "mon", "fri"]))
    expect("a day said twice is one day", Planner.weekdays(inAnswer: ["mon", "Mon"]),
           Planner.Draft.Days.named(["mon"]))
    // `when.days` is an allowlist in the parser, so a word that is not one of the seven has to be
    // dropped here — carried into the form it would be a field somebody has to clear.
    expect("a word that is not a day is dropped", Planner.weekdays(inAnswer: ["weekdays"]),
           Planner.Draft.Days.unsaid)
    expect("and a number among them", Planner.weekdays(inAnswer: ["mon", 3]),
           Planner.Draft.Days.named(["mon"]))
    expect("an empty list says the sentence named none",
           Planner.weekdays(inAnswer: [String]()),
           Planner.Draft.Days.unsaid)
    expect("and so does a missing field", Planner.weekdays(inAnswer: nil),
           Planner.Draft.Days.unsaid)
    expect("every day wins over a day named beside it",
           Planner.weekdays(inAnswer: ["mon", "daily"]), Planner.Draft.Days.daily)

    // What goes on the wire is the shape POST /v1/orchestrator/schedules takes, so a draft can be
    // carried into that form without being translated on the way.
    expect("daily is a string on the wire", Planner.Draft.Days.daily.payload as? String, "daily")
    expect("named days are an array", Planner.Draft.Days.named(["mon"]).payload as? [String],
           ["mon"])
    expect("and nothing said is an empty one",
           Planner.Draft.Days.unsaid.payload as? [String], [])
}

group("a number the planner wrote is mapped back to a place, and nothing else is") {
    expect("one is the first", Planner.place(number: 1, in: plannerPlaces)?.path,
           "/Users/me/code/clawdline")
    expect("two is the second", Planner.place(number: 2, in: plannerPlaces)?.path,
           "/Users/me/code/notebook")
    expect("and the last number is the last row", Planner.place(number: 3, in: plannerPlaces)?.path,
           "/Users/me/code/cairn")

    // Zero is how the prompt spells "none of these fits", so it has to be nothing rather than
    // the first row — an off-by-one here sends work to whichever project was worked in last.
    expect("zero is not the first project", Planner.place(number: 0, in: plannerPlaces)?.path, nil)
    expect("nor is a negative one", Planner.place(number: -1, in: plannerPlaces)?.path, nil)
    // A model that answered 4 for a list of 3 has not picked the last one — it has picked
    // something that is not there, and clamping would have been an invented answer.
    expect("one past the end is nothing", Planner.place(number: 4, in: plannerPlaces)?.path, nil)
    expect("and so is a number nowhere near it",
           Planner.place(number: 999, in: plannerPlaces)?.path, nil)
    expect("an empty list has no first row either", Planner.place(number: 1, in: [])?.path, nil)
}

group("what a model printed becomes a draft, or does not") {
    func object(_ json: String) -> [String: Any] {
        ((try? JSONSerialization.jsonObject(with: Data(json.utf8))) as? [String: Any]) ?? [:]
    }
    let good = """
    {"project":2,"assistant":"codex","instructions":"Run the tests and paste anything red",\
    "title":"Run the tests","confidence":0.9,"question":""}
    """

    // The shape `claude -p --output-format json` prints. `structured_output` is the half that
    // went through the schema, and it is what this reads when it is there.
    let envelope = """
    {"type":"result","is_error":false,"duration_ms":4600,"result":"ignored",\
    "structured_output":\(good)}
    """
    let fromEnvelope = Planner.object(inClaudeOutput: envelope)
    expect("the schema-validated object is the one taken",
           (fromEnvelope?["assistant"] as? String), "codex")

    // A run whose schema did not stick still prints the text, and that text is an answer.
    let textOnly = """
    {"type":"result","result":\(String(data: try! JSONSerialization.data(withJSONObject: good, options: .fragmentsAllowed), encoding: .utf8)!)}
    """
    expect("and `result` is read when it is not there",
           (Planner.object(inClaudeOutput: textOnly)?["title"] as? String), "Run the tests")

    // Measured on other one-shot turns in this app: a model asked for JSON writes a fence around
    // it about as often as not, and a fence is not a failure to answer.
    let fenced = "```json\n\(good)\n```"
    expect("a markdown fence is not an answer this throws away",
           (Planner.object(inText: fenced)?["title"] as? String), "Run the tests")
    expect("nor is a fence with no language on it",
           (Planner.object(inText: "```\n\(good)\n```")?["title"] as? String), "Run the tests")
    expect("nor a sentence in front of the object",
           (Planner.object(inText: "Here is the draft:\n\(good)")?["title"] as? String),
           "Run the tests")

    // Half an object is not a draft, and this is the case that matters: a turn killed at its
    // deadline leaves exactly this behind, and a parser that repaired it would be inventing the
    // half that never arrived.
    expect("a truncated answer is nothing", Planner.object(inText: String(good.dropLast(20)))?.count, nil)
    expect("and so is one cut inside a string",
           Planner.object(inText: #"{"project":2,"instructions":"Run the te"#)?.count, nil)
    expect("prose that never became an object is nothing",
           Planner.object(inText: "I could not work out which project you meant.")?.count, nil)
    expect("and neither is nothing at all", Planner.object(inClaudeOutput: "")?.count, nil)

    // Every field is checked on the way in, because the fallback engine's answer went through no
    // schema at all — `codex exec` is handed the shape in words.
    let draft = Planner.draft(from: object(good), places: plannerPlaces)
    expect("the number became the second project's id", draft?.placeID, plannerPlaces[1].id)
    expect("the assistant is the one named", draft?.assistant, Assistant.codex)
    expect("and the first message came through", draft?.instructions,
           "Run the tests and paste anything red")

    let offList = Planner.draft(from: object("""
    {"project":9,"assistant":"claude","instructions":"do a thing","title":"t","confidence":0.9,\
    "question":""}
    """), places: plannerPlaces)
    expect("a project that is not on the list is no project", offList?.placeID, nil)
    check("and the draft survives to be read anyway", offList != nil)

    let invented = Planner.draft(from: object("""
    {"project":1,"assistant":"emacs","instructions":"do a thing","title":"t","confidence":0.9,\
    "question":""}
    """), places: plannerPlaces)
    expect("an assistant nobody has heard of is not passed on", invented?.assistant,
           Assistant.claude)

    // An empty first message is a request, not a missing field: "open clawdline" and nothing
    // else asks for a session with nothing typed into it. The whitespace goes, the draft stays,
    // and the page opens a tab and sends no message.
    let silent = Planner.draft(from: object("""
    {"project":1,"assistant":"claude","instructions":"   ","title":"t","confidence":0.9,\
    "question":""}
    """), places: plannerPlaces)
    expect("a draft with nothing to say still names a place", silent?.placeID,
           plannerPlaces.first?.id)
    expect("and its first message is empty rather than blank", silent?.instructions, "")

    // How big the session should be, checked on the way in like everything else here. A size
    // that is not one of the three is the empty string rather than a guess: a wrong guess opens
    // a session on a model nobody asked for, and nothing on screen says which one it is.
    func sized(_ assistant: String, _ model: Any?) -> Planner.Draft? {
        var body: [String: Any] = ["project": 1, "assistant": assistant,
                                   "instructions": "do a thing", "title": "t",
                                   "confidence": 0.9, "question": ""]
        if let model { body["model"] = model }
        return Planner.draft(from: body, places: plannerPlaces)
    }
    expect("a size the schema offers comes through", sized("claude", "opus")?.model, "opus")
    expect("so does the smallest", sized("claude", "haiku")?.model, "haiku")
    expect("and one written in capitals is the same size",
           sized("claude", "Sonnet")?.model, "sonnet")
    expect("a model nobody has heard of is no model", sized("claude", "gpt-9")?.model, "")
    expect("nor is a dated build of one of the three",
           sized("claude", "claude-opus-5-20260201")?.model, "")
    expect("a number where the name goes is not one", sized("claude", 5)?.model, "")
    expect("and a field the model left out is no model", sized("claude", nil)?.model, "")
    // Codex expresses how hard a job is as reasoning effort, set when work is handed to it.
    // Decided here as well as in the prompt, because this is the side that cannot be talked
    // out of it.
    expect("codex names no model, whatever it wrote", sized("codex", "opus")?.model, "")
    expect("and an unknown assistant becomes claude, which may", sized("emacs", "opus")?.model,
           "opus")

    // The wire contract: the page carries this into the fourth segment of
    // `POST /v1/places/:id/start/:assistant/:model`, so it has to be on the payload.
    expect("the size is on the wire", sized("claude", "opus")?.payload["model"] as? String,
           "opus")
    expect("and an empty one is an empty string rather than a missing field",
           sized("codex", "opus")?.payload["model"] as? String, "")
}

group("how sure the planner was decides whether the page asks") {
    func draft(_ confidence: Double, question: String) -> Planner.Draft? {
        Planner.draft(from: ["project": 1, "assistant": "claude", "instructions": "do a thing",
                             "title": "a title", "confidence": confidence, "question": question],
                      places: plannerPlaces)
    }

    // Below the line the question is what the page shows; at or above it there is nothing to
    // ask, and a model that wrote a question anyway would have the page asking about a draft it
    // had no doubts about.
    expect("a guess keeps its question", draft(0.3, question: "which project?")?.question,
           "which project?")
    expect("and the threshold itself counts as sure",
           draft(Planner.sure, question: "which project?")?.question, "")
    expect("as does anything above it", draft(0.95, question: "which project?")?.question, "")
    expect("a confident draft with no question was never going to ask",
           draft(0.9, question: "")?.question, "")

    // A model asked for a number between nought and one will occasionally write 12. Clamped
    // rather than refused: the number is a hint on a draft somebody is about to read, and
    // throwing the draft away over it would be the worse answer.
    expect("a confidence past one is one", draft(12, question: "")?.confidence, 1)
    expect("and one below nought is nought", draft(-3, question: "x")?.confidence, 0)
    expect("nought is still a guess, so it still asks", draft(-3, question: "x")?.question, "x")

    // A schedule with no time is not one anybody can save, so however sure the model said it was,
    // the page has to ask rather than offer a Save button. Decided on this side as well as in the
    // prompt: the two disagreeing would be a draft claiming to be sure of a time it does not have.
    func timed(_ kind: String, at: String, confidence: Double,
               question: String = "") -> Planner.Draft? {
        Planner.draft(from: ["project": 1, "assistant": "claude", "instructions": "run the tests",
                             "title": "a title", "confidence": confidence, "question": question,
                             "kind": kind, "at": at, "days": ["daily"]],
                      places: plannerPlaces)
    }
    check("a schedule with no time is never offered as an answer",
          (timed("schedule", at: "", confidence: 0.95)?.confidence ?? 1) < Planner.sure)
    expect("and the question the model wrote about it survives",
           timed("schedule", at: "", confidence: 0.95, question: "what time?")?.question,
           "what time?")
    expect("a schedule that does have one keeps what the model said",
           timed("schedule", at: "09:00", confidence: 0.95)?.confidence, 0.95)
    expect("and a session with no time is not a schedule with a missing one",
           timed("session", at: "", confidence: 0.95)?.confidence, 0.95)
}

group("a draft says whether the sentence asked for a session now or one that repeats") {
    func draft(_ object: [String: Any]) -> Planner.Draft? {
        var body: [String: Any] = ["project": 1, "assistant": "claude",
                                   "instructions": "run the tests", "title": "a title",
                                   "confidence": 0.9, "question": ""]
        for (key, value) in object { body[key] = value }
        return Planner.draft(from: body, places: plannerPlaces)
    }

    let repeating = draft(["kind": "schedule", "at": "9:00",
                           "days": ["fri", "mon"]])
    expect("a schedule says so", repeating?.kind, Planner.Draft.Kind.schedule)
    expect("its time arrives in the spelling the schedule file takes", repeating?.at, "09:00")
    expect("and its days in week order", repeating?.days,
           Planner.Draft.Days.named(["mon", "fri"]))

    // The default is the cheaper mistake: a session nobody wanted is one tab to close, and a
    // schedule nobody wanted opens one every day until somebody finds it.
    expect("a draft that says nothing about it is a session", draft([:])?.kind,
           Planner.Draft.Kind.session)
    expect("and so is a word that is neither", draft(["kind": "cron"])?.kind,
           Planner.Draft.Kind.session)
    expect("a session names no time", draft([:])?.at, "")
    expect("and no days", draft([:])?.days, Planner.Draft.Days.unsaid)

    // The wire contract. The page carries these three straight into the form that posts to
    // /v1/orchestrator/schedules, so they have to arrive in the shape that route takes.
    let payload = repeating?.payload ?? [:]
    expect("kind is on the wire", payload["kind"] as? String, "schedule")
    expect("so is the time", payload["at"] as? String, "09:00")
    expect("and the days, as the array that route takes", payload["days"] as? [String],
           ["mon", "fri"])
    expect("every day is that route's string rather than seven names",
           (draft(["kind": "schedule", "at": "07:00", "days": "daily"])?.payload["days"]) as? String,
           "daily")
    expect("and a session's days are an empty array rather than a missing field",
           draft([:])?.payload["days"] as? [String], [])
}

group("the sentence POST /v1/intents plans from, before it costs a model turn") {
    func refusal(_ body: [String: Any]) -> String {
        guard case .refused(let response) = RemoteServer.intent(from: body) else { return "" }
        return remoteErrorCode(response)
    }
    func sentence(_ body: [String: Any]) -> String? {
        guard case .text(let text) = RemoteServer.intent(from: body) else { return nil }
        return text
    }

    expect("no text at all is a bad request", refusal([:]), "bad_request")
    expect("and neither is a number one", refusal(["text": 7]), "bad_request")
    expect("an empty one too", refusal(["text": ""]), "bad_request")
    expect("and one that is only whitespace", refusal(["text": "  \n\t "]), "bad_request")

    expect("an ordinary sentence gets through",
           sentence(["text": "run the tests in clawdline"]), "run the tests in clawdline")
    expect("and arrives trimmed",
           sentence(["text": "  run the tests \n"]), "run the tests")

    // The limit is in bytes rather than characters, because what it is protecting is what a
    // model is paid to read — and a Chinese sentence is three bytes a character.
    let long = String(repeating: "a", count: RemoteServer.intentLimit)
    expect("exactly the limit is allowed", sentence(["text": long])?.count, RemoteServer.intentLimit)
    expect("one byte past it is not", refusal(["text": long + "a"]), "bad_request")
    let chinese = String(repeating: "字", count: RemoteServer.intentLimit / 3 + 1)
    check("and a sentence counted in characters would have slipped past",
          chinese.count <= RemoteServer.intentLimit && chinese.utf8.count > RemoteServer.intentLimit)
    expect("so it is counted in bytes", refusal(["text": chinese]), "bad_request")
}

group("a deploy is news only when it stops running") {
    // The whole feature is one rule applied to two readings, and every way of getting it wrong
    // is a phone buzzing about something that did not just happen.
    func changed(_ before: [String: String], _ after: [String: String]) -> [String] {
        DeployWatch.finished(from: before, to: after).map { "\($0.repo):\($0.ok)" }
    }

    expect("running to ok is a success",
           changed(["a": "running"], ["a": "ok"]), ["a:true"])
    expect("running to fail is a failure",
           changed(["a": "running"], ["a": "fail"]), ["a:false"])
    expect("still running is nothing",
           changed(["a": "running"], ["a": "running"]), [])

    // The one that would have made this lie. A repo read for the first time has no previous
    // state, and every deploy that ever finished is sitting in that file waiting to be
    // announced — so opening the app on a Monday would report Friday's deploy as news.
    expect("a first reading is not a transition",
           changed([:], ["a": "ok"]), [])
    expect("a first reading of a failure is not a transition either",
           changed([:], ["a": "fail"]), [])

    // Starting one is not news: you started it.
    expect("ok to running is nothing", changed(["a": "ok"], ["a": "running"]), [])
    expect("an outcome that has not changed is nothing",
           changed(["a": "fail"], ["a": "fail"]), [])

    // A state nobody has defined must not read as an outcome by accident — `!= "running"` is
    // the test, so anything unrecognised lands in the failure branch rather than being dropped.
    expect("an unknown state that follows running still counts, as a failure",
           changed(["a": "running"], ["a": "cancelled"]), ["a:false"])

    // Reading several projects at once, which is the normal case.
    expect("each repo is judged on its own",
           changed(["a": "running", "b": "running", "c": "ok"],
                   ["a": "ok", "b": "running", "c": "running"]), ["a:true"])

    // A project whose status file went away — the reading simply does not mention it, and a
    // repo that is not in the new reading has not finished anything.
    expect("a repo that disappears says nothing",
           changed(["a": "running"], [:]), [])
}

group("answering a menu or cycling permissions stays a closed key allowlist") {
    // This is the only path in the app that writes a raw byte into a tty from the network, so the
    // allowlist is the whole security argument. It lives in `Targets.answer`, not at the route:
    // a second route added later would otherwise have to remember to repeat it.
    let session = TargetSession(backend: .tmux, id: "%nope%", name: "x", tty: "/dev/ttys99",
                                windowIndex: 0, tabIndex: 0, assistant: .claude)

    for bad: UInt8 in [0x1b, 0x0d, 0x0a, 0x03, 0x30, 0x41, 0x7f, 0x00] {
        check("byte \(bad) is refused before it reaches a terminal",
              Targets.answer(bad, to: session) == "That is not a key this can send.")
    }

    // The allowed ones are not exercised here on purpose — they would run osascript against a
    // session that does not exist. What is asserted is that they get *past* the allowlist, which
    // is the only thing this function decides.
    for good: UInt8 in [0x31, 0x39, 0x09] {
        check("byte \(good) is not refused by the allowlist",
              Targets.answer(good, to: session) != "That is not a key this can send.")
    }
    check("back-tab is the one multi-byte sequence admitted",
          Targets.answer([0x1b, 0x5b, 0x5a], to: session) != "That is not a key this can send.")
    for bad in [[UInt8](), [0x1b], [0x5b, 0x5a], [0x1b, 0x5b, 0x41], [0x31, 0x32]] {
        check("sequence \(bad) is refused before it reaches a terminal",
              Targets.answer(bad, to: session) == "That is not a key this can send.")
    }
}

group("the key route is gated like every other write") {
    let wasWriting = Config.shared.remoteWrite
    defer { Config.shared.remoteWrite = wasWriting }

    let reader = RemoteAuth.addDevice(name: "a phone that may read", caps: [.read])
    let writer = RemoteAuth.addDevice(name: "a phone that may send", caps: [.read, .send])
    defer { RemoteAuth.revoke(id: reader.id); RemoteAuth.revoke(id: writer.id) }

    // A fresh idempotency key every time, or the second call with the same body would be answered
    // out of the ten-minute cache and assert nothing. The first version of this test omitted the
    // header entirely, and every case passed — as a 400 for the missing header rather than for the
    // reason it claimed. A green check that is green for the wrong reason is worse than a red one.
    func key(_ token: String?, _ body: String,
             idempotency: String? = nil) -> RemoteServer.Response {
        var headers: [String: String] = [:]
        if let token { headers["Authorization"] = "Bearer \(token)" }
        headers["Content-Type"] = "application/json"
        headers["Idempotency-Key"] = idempotency ?? UUID().uuidString
        return RemoteServer.shared.route(
            remoteRequest("POST", "/v1/sessions/%nope%/key", headers: headers, body: body))
    }

    Config.shared.remoteWrite = true
    let anonymous = key(nil, "{\"key\":\"1\"}")
    expect("no token is refused", anonymous.status, 401)

    let readOnly = key(reader.token, "{\"key\":\"1\"}")
    expect("a device that may only read cannot answer a menu", readOnly.status, 403)

    Config.shared.remoteWrite = false
    expect("the write switch is checked first",
           remoteErrorCode(key(writer.token, "{\"key\":\"1\"}")), "write_disabled")

    Config.shared.remoteWrite = true
    // Anything that is not a menu key is refused at the parse, before a session is even looked up
    // — so the shape of the request cannot be used to probe which sessions exist.
    for bad in ["0", "a", "", "escape", "10", "11", "\\u001b", "1 "] {
        let out = key(writer.token, "{\"key\":\"\(bad)\"}")
        expect("key \"\(bad)\" is a bad request", out.status, 400)
    }
    expect("a missing key is too", key(writer.token, "{}").status, 400)

    // The header the write gate insists on, checked here too because this route is the one where a
    // retry is not harmless: the same digit arriving twice answers a question and then types a
    // stray character into whatever replaced it.
    var noKey: [String: String] = ["Authorization": "Bearer \(writer.token)"]
    noKey["Content-Type"] = "application/json"
    expect("and an answer without an Idempotency-Key is refused",
           RemoteServer.shared.route(remoteRequest("POST", "/v1/sessions/%nope%/key",
                                                   headers: noKey,
                                                   body: "{\"key\":\"1\"}")).status, 400)

    // Retried with the same key, the stored answer comes back rather than a second keystroke.
    let once = UUID().uuidString
    let first = key(writer.token, "{\"key\":\"2\"}", idempotency: once)
    let again = key(writer.token, "{\"key\":\"2\"}", idempotency: once)
    expect("a retry is the stored answer, not a second press", again.status, first.status)

    // A well-formed key against a session that is not there is a 404, which means the parse ran
    // first and the allowlist did its job before anything went looking for a terminal.
    expect("a good key against no session is a 404",
           key(writer.token, "{\"key\":\"3\"}").status, 404)
    expect("and tab is a good key", key(writer.token, "{\"key\":\"tab\"}").status, 404)
    expect("and shift+tab is a good key",
           key(writer.token, "{\"key\":\"shift+tab\"}").status, 404)
    // A multi-select's button is pressed by name rather than by number, because it has no number
    // on screen. It goes through the same parse and the same gates as every other key.
    expect("and submit is a good key",
           key(writer.token, "{\"key\":\"submit\"}").status, 404)
}

group("the session-title route is a bounded authenticated write") {
    let wasWriting = Config.shared.remoteWrite
    defer { Config.shared.remoteWrite = wasWriting }
    let reader = RemoteAuth.addDevice(name: "a title reader", caps: [.read])
    let writer = RemoteAuth.addDevice(name: "a title writer", caps: [.read, .send])
    defer { RemoteAuth.revoke(id: reader.id); RemoteAuth.revoke(id: writer.id) }

    func title(_ token: String?, _ value: Any) -> RemoteServer.Response {
        var headers = ["Content-Type": "application/json",
                       "Idempotency-Key": UUID().uuidString]
        if let token { headers["Authorization"] = "Bearer \(token)" }
        let body = try! JSONSerialization.data(withJSONObject: ["title": value])
        return RemoteServer.shared.route(remoteRequest(
            "POST", "/v1/sessions/missing/title", headers: headers,
            body: String(decoding: body, as: UTF8.self)))
    }

    Config.shared.remoteWrite = true
    expect("an anonymous title write is refused", title(nil, "name").status, 401)
    expect("a read-only device cannot name a session", title(reader.token, "name").status, 403)
    Config.shared.remoteWrite = false
    expect("the global write switch protects session titles",
           remoteErrorCode(title(writer.token, "name")), "write_disabled")
    Config.shared.remoteWrite = true
    expect("a well-formed title for no session is a 404",
           title(writer.token, "name").status, 404)
    expect("a non-string title is rejected before session lookup",
           title(writer.token, 7).status, 400)
    expect("an overlong title is rejected before session lookup",
           title(writer.token, String(repeating: "x", count: Config.sessionTitleLimit + 1)).status,
           400)
}

group("a recording arrives as base64 and is refused before it costs a second of CPU") {
    // Little-endian 16-bit mono, which is the only shape /v1/voice takes -- and the shape the
    // bar's own recorder converts to before it calls the same transcriber. Two bytes a sample.
    func spoken(seconds: Double) -> Data {
        var out = Data()
        for i in 0..<Int(RemoteServer.voiceRate * seconds) {
            withUnsafeBytes(of: Int16(truncatingIfNeeded: i &* 37).littleEndian) {
                out.append(contentsOf: $0)
            }
        }
        return out
    }
    // The same length without the arithmetic, for the cases that are about size and not content.
    func bytes(seconds: Double) -> Data {
        Data(repeating: 7, count: Int(RemoteServer.voiceRate * seconds) * 2)
    }
    func body(_ audio: Data, rate: Any? = 16_000) -> [String: Any] {
        var out: [String: Any] = ["audio": audio.base64EncodedString()]
        if let rate { out["rate"] = rate }
        return out
    }
    func samples(_ body: [String: Any]) -> Data? {
        if case .samples(let data) = RemoteServer.voiceSamples(from: body) { return data }
        return nil
    }
    func refusal(_ body: [String: Any]) -> RemoteServer.Response? {
        if case .refused(let response) = RemoteServer.voiceSamples(from: body) { return response }
        return nil
    }

    // The whole of the encoding's job: what the phone recorded is what whisper is handed, byte
    // for byte. A round trip that dropped one byte would shift every sample after it by half a
    // sample, and the recording would transcribe as noise rather than fail.
    let recording = spoken(seconds: 1)
    expect("a second of audio is 16000 samples", recording.count, 32_000)
    expect("and survives base64 whole", samples(body(recording)), recording)

    // An encoder that wraps its lines is not a client making a mistake, and neither is one that
    // pads with a newline at the end.
    let encoded = Array(recording.base64EncodedString())
    let wrapped = stride(from: 0, to: encoded.count, by: 76)
        .map { String(encoded[$0..<min($0 + 76, encoded.count)]) }
        .joined(separator: "\n")
    expect("wrapped base64 is the same recording",
           samples(["audio": wrapped, "rate": 16_000]), recording)

    // Every refusal below is the same code on purpose: they are one kind of mistake -- a body
    // this cannot read -- and a route that answered three codes for one condition would make a
    // page write three cases out of it.
    expect("a missing audio field is a bad request", refusal(["rate": 16_000])?.status, 400)
    expect("and says so in the code", remoteErrorCode(refusal(["rate": 16_000])!), "bad_request")
    expect("an empty string is not audio",
           refusal(["audio": "", "rate": 16_000])?.status, 400)
    expect("and neither is something that is not base64 at all",
           refusal(["audio": "!!!!", "rate": 16_000])?.status, 400)

    // 16 kHz is what whisper wants and what the recorder produces, so a body naming another rate
    // has not made a small mistake: resampled by hand it would come out three times too fast, and
    // resampling it here quietly would hide that from whoever sent it.
    for wrong in [8_000, 22_050, 44_100, 48_000, 16_001] {
        expect("rate \(wrong) is refused", refusal(body(recording, rate: wrong))?.status, 400)
    }
    expect("a rate given as a string is not a rate",
           refusal(body(recording, rate: "16000"))?.status, 400)
    expect("and a body with no rate at all is refused rather than assumed",
           refusal(body(recording, rate: nil))?.status, 400)
    check("16000 written as a decimal is still 16000",
          samples(body(recording, rate: 16_000.0)) == recording)

    // The floor is the one whisper keeps for itself, said here as well so the answer can explain
    // itself: whisper's way of refusing is nil, which this route would have to report as silence.
    expect("a tenth of a second is a tap, not a sentence",
           refusal(body(bytes(seconds: 0.1)))?.status, 400)
    expect("and so is nothing at all", refusal(body(Data()))?.status, 400)
    check("a quarter of a second is exactly enough", samples(body(bytes(seconds: 0.25))) != nil)
    check("and so is a second and a half", samples(body(bytes(seconds: 1.5))) != nil)

    // The far end is about what it costs rather than about the shape of the request: whisper runs
    // at something near real time, so a long upload holds the queue with nobody able to call it
    // back.
    check("five minutes is still allowed", samples(body(bytes(seconds: 300))) != nil)
    expect("a little over is not", refusal(body(bytes(seconds: 301)))?.status, 400)

    // One running, one waiting. The third is told to come back rather than answered five seconds
    // after it was spoken, by which time whoever spoke it has pressed the button again.
    expect("the queue is two deep", RemoteServer.voiceDepth, 2)
}

group("Git porcelain and numstat become the web status payload") {
    let status = """
    # branch.oid d5c61e9f91c46a77
    # branch.head main
    # branch.upstream origin/main
    # branch.ab +2 -3
    1 MM N... 100644 100644 100644 aaaaaaa bbbbbbb Sources/Foo.swift
    2 R. N... 100644 100644 100644 ccccccc ddddddd R100 Sources/New Name.swift\tSources/Old Name.swift
    1 A. N... 000000 100644 100644 0000000 eeeeeee Sources/Added.swift
    1 .D N... 100644 100644 000000 fffffff fffffff Sources/Gone.swift
    1 .M N... 100644 100644 100644 1111111 2222222 Assets/blob.png
    ? Notes/new file.txt
    u UU N... 100644 100644 100644 100644 3333333 4444444 5555555 Sources/Conflict.swift
    """
    let unstaged = """
    7\t2\tSources/Foo.swift
    -\t-\tAssets/blob.png
    2\t2\tSources/Conflict.swift
    """
    let staged = """
    5\t1\tSources/Foo.swift
    1\t0\tSources/{Old Name => New Name}.swift
    3\t0\tSources/Added.swift
    0\t4\tSources/Gone.swift
    """

    let parsed = GitChanges.assemble(status: status, unstaged: unstaged, staged: staged)
    expect("the branch is parsed", parsed.branch, "main")
    expect("the object id is parsed", parsed.head, "d5c61e9f91c46a77")
    expect("ahead is parsed", parsed.ahead, 2)
    expect("behind is parsed", parsed.behind, 3)
    expect("every porcelain row is kept", parsed.files.count, 7)

    func file(_ path: String) -> GitChanges.File? {
        parsed.files.first { $0.path == path }
    }
    let partial = file("Sources/Foo.swift")
    expect("a partially staged file is staged", partial?.staged, true)
    expect("and is unstaged", partial?.unstaged, true)
    expect("its two addition counts are added", partial?.additions, 12)
    expect("and its two deletion counts are added", partial?.deletions, 3)

    let renamed = file("Sources/New Name.swift")
    expect("a rename keeps its destination", renamed?.kind, .renamed)
    expect("a rename keeps its source", renamed?.from, "Sources/Old Name.swift")
    expect("a braced numstat rename joins the destination", renamed?.additions, 1)
    expect("an index-only rename is staged", renamed?.staged, true)
    expect("an index-only rename is not unstaged", renamed?.unstaged, false)

    expect("an added row is added", file("Sources/Added.swift")?.kind, .added)
    expect("a deleted row is deleted", file("Sources/Gone.swift")?.kind, .deleted)
    let untracked = file("Notes/new file.txt")
    expect("an untracked path may contain spaces", untracked?.kind, .untracked)
    expect("untracked means a worktree change", untracked?.unstaged, true)
    expect("an untracked file has no invented count", untracked?.additions, nil)
    expect("an unmerged row is a conflict", file("Sources/Conflict.swift")?.kind, .conflict)
    expect("a binary addition count is null", file("Assets/blob.png")?.additions, nil)
    expect("and so is its deletion count", file("Assets/blob.png")?.deletions, nil)

    let payload = GitChanges.payload(parsed)
    let git = payload["git"] as? [String: Any]
    expect("a payload with files is not clean", git?["clean"] as? Bool, false)
    expect("the payload carries every file", (git?["files"] as? [[String: Any]])?.count, 7)
    let clean = GitChanges.payload(GitChanges.parseStatus(
        "# branch.oid abc\n# branch.head topic\n# branch.ab +0 -0\n"))
    expect("an empty status payload is clean",
           (clean["git"] as? [String: Any])?["clean"] as? Bool, true)
}

group("every word the page can draw is a word the page is sent") {
    // **This one is here because the same mistake happened twice.** A string gets added to `Copy`,
    // translated into fourteen languages, and then not listed in `strings(for:)` — and nothing
    // breaks, because the page carries an English copy of everything as a fallback. So the failure
    // is invisible to whoever made it and visible only to somebody reading one of the other
    // thirteen languages. The second time, the string left behind was the warning that sending
    // from a phone confirms the wrong menu option.
    //
    // Walking `Copy` rather than listing names, for the same reason the translation check walks
    // it: a list you have to remember to extend is the thing that failed.
    let payload = RemoteServer.shared.route(remoteRequest("GET", "/v1/strings"))
    let sent = ((try? JSONSerialization.jsonObject(with: payload.body)) as? [String: Any]) ?? [:]
    let fallbackSource = (try? String(contentsOfFile: "Resources/web/app/js/core/i18n.js",
                                      encoding: .utf8)) ?? ""
    let fallbackObject = fallbackSource.components(separatedBy: "export var T = {").dropFirst()
        .first?.components(separatedBy: "\n};").first ?? ""
    let fallbackKeys = Set(fallbackObject.split(separator: "\n").compactMap { line -> String? in
        let parts = line.split(separator: ":", maxSplits: 1)
        guard parts.count == 2 else { return nil }
        let key = parts[0].trimmingCharacters(in: .whitespaces)
        return key.hasPrefix("web") ? key : nil
    })

    // Members the page is deliberately not given. Each one needs a reason written here, and
    // "the page does not use it yet" is not a reason — an unused string should not be in `Copy`.
    let notSent: Set<String> = []

    // Keys that are sent without being members. Legitimate when a *function* on `Copy` supplies
    // them — a question with two answers does not cross a JSON boundary as one — or when a member
    // the Mac's own screen already draws is the same word here, which is a translation not made a
    // second time rather than a string nobody can change.
    let derived: Set<String> = ["webOrderNewest", "webOrderOldest",  // t.outputOrder(newestFirst:)
                                "webScheduleNext",                  // t.settingsScheduleNext
                                "webImageExpired", "webImagePreview", "webImageClose"]

    var missing: [String] = []
    for child in Mirror(reflecting: English()).children {
        guard let label = child.label, label.hasPrefix("web"), child.value is String else { continue }
        if notSent.contains(label) { continue }
        if sent[label] == nil { missing.append(label) }
    }
    check("no web string is translated and then never sent", missing.isEmpty,
          "in Copy but not in /v1/strings: " + missing.sorted().joined(separator: ", "))

    // And the other direction, which is the cheaper mistake but still a mistake: a key the page
    // is sent that no longer exists on `Copy` is a key nothing can ever change again.
    let known = Set(Mirror(reflecting: English()).children.compactMap { $0.label })
    let orphans = sent.keys.filter {
        $0.hasPrefix("web") && !known.contains($0) && !derived.contains($0)
    }
    check("and nothing is sent under a name Copy does not have", orphans.isEmpty,
          "sent but not in Copy: " + orphans.sorted().joined(separator: ", "))

    // These three predate structured notices and are already absent in v2. Fixing their fallback
    // copy requires changing i18n.js, which is deliberately outside this narrow correction.
    let knownMissingFallbacks: Set<String> = [
        "webSettingsAssistantIcons", "webSettingsAssistantIconsSay",
        "webSettingsAssistantIconsShow",
    ]
    let absentFallbacks = sent.keys.filter {
        $0.hasPrefix("web") && !knownMissingFallbacks.contains($0) && !fallbackKeys.contains($0)
    }
    check("and every in-scope browser string sent by the server has a fallback slot",
          absentFallbacks.isEmpty,
          "sent but absent from i18n.js T: " + absentFallbacks.sorted().joined(separator: ", "))
}

group("health and hello carry the same fields their shared handler reads") {
    // `/v1/health` also has route-only facts (`ok`, `auth`, `password`, `authed`) which an SSE
    // frame cannot all compute: the stream no longer has the Request that proved authentication.
    // The contract is the set consumed by both paths through handlers.hello. It is derived from
    // those reads rather than repeated here, because `build` was once added to health and to the
    // consumer but not to hello; a hand-maintained expected list would recreate that exact bug.
    let frontendPaths = ["Resources/web/app/js/net/build.js",
                         "Resources/web/app/js/net/handlers.js"]
    // Reading these with `try?` and dropping whatever failed is how a contract that grows by
    // itself also shrinks by itself: rename net/build.js and `build` and `protocol` — the two
    // keys ece9e74 turned on — leave `consumed` without a word, and this stays green with the
    // original defect back in the tree. Reading none of them is the history from before the
    // JS split, where index.html is the whole consumer; reading some of them is a rename this
    // list has not been told about, and that has to be louder than a smaller answer.
    let frontendSources = frontendPaths.map {
        (path: $0, text: try? String(contentsOfFile: $0, encoding: .utf8))
    }
    let unreadable = frontendSources.filter { $0.text == nil }.map { $0.path }
    check("every file the consumed set is read from is there",
          unreadable.isEmpty || unreadable.count == frontendPaths.count,
          "could not read: " + unreadable.joined(separator: ", "))
    let frontend = frontendSources.compactMap { $0.text }.joined(separator: "\n")
    let legacy = (try? String(contentsOfFile: "Resources/web/index.html", encoding: .utf8)) ?? ""
    let consumer = frontend.isEmpty ? legacy : frontend
    let pattern = try! NSRegularExpression(pattern: #"\binfo\.([A-Za-z_$][A-Za-z0-9_$]*)"#)
    let whole = NSRange(consumer.startIndex..<consumer.endIndex, in: consumer)
    let consumed = Set(pattern.matches(in: consumer, range: whole).compactMap { match -> String? in
        guard let range = Range(match.range(at: 1), in: consumer) else { return nil }
        return String(consumer[range])
    })

    let response = RemoteServer.shared.route(remoteRequest("GET", "/v1/health"))
    let health = Set((((try? JSONSerialization.jsonObject(with: response.body))
        as? [String: Any]) ?? [:]).keys)

    let hello = Set(RemoteServer.restartHelloPayload().keys)

    let healthFields = health.intersection(consumed)
    let helloFields = hello.intersection(consumed)
    let healthMissing = consumed.subtracting(health).sorted()
    let helloMissing = consumed.subtracting(hello).sorted()
    check("the health answer and hello event agree on every client-state key",
          !consumed.isEmpty && healthFields == consumed && helloFields == consumed,
          "consumed: \(consumed.sorted()); health missing: \(healthMissing); hello missing: \(helloMissing)")
}

group("ending a session is the one route that destroys something") {
    let wasWriting = Config.shared.remoteWrite
    defer { Config.shared.remoteWrite = wasWriting }

    let reader = RemoteAuth.addDevice(name: "a phone that may read", caps: [.read])
    let writer = RemoteAuth.addDevice(name: "a phone that may send", caps: [.read, .send])
    defer { RemoteAuth.revoke(id: reader.id); RemoteAuth.revoke(id: writer.id) }

    // **Every case here is a refusal.** There is no test of the path that works, because the
    // path that works closes a terminal tab, and a suite that occasionally ends somebody's
    // session is a suite people stop running. What is asserted is that nothing reaches
    // `Targets.end` without passing the same gate as every other write.
    func end(_ token: String?, key: Bool = true) -> RemoteServer.Response {
        var headers: [String: String] = [:]
        if let token { headers["Authorization"] = "Bearer \(token)" }
        if key { headers["Idempotency-Key"] = UUID().uuidString }
        return RemoteServer.shared.route(
            remoteRequest("POST", "/v1/sessions/%nope%/end", headers: headers))
    }

    Config.shared.remoteWrite = true
    expect("no token is refused", end(nil).status, 401)
    expect("a device that may only read cannot end one", end(reader.token).status, 403)
    expect("and without an Idempotency-Key it is a bad request",
           end(writer.token, key: false).status, 400)

    Config.shared.remoteWrite = false
    expect("the write switch is checked first",
           remoteErrorCode(end(writer.token)), "write_disabled")

    Config.shared.remoteWrite = true
    expect("a session that is not there is a 404", end(writer.token).status, 404)
}

group("a numbered list somebody typed is not a menu") {
    // Taken from a real capture, 2026-08-19. `\u{276F}` is both the glyph a dialog marks its
    // current row with and the one Claude Code puts in front of the line you type — so a message
    // that opens with a numbered list echoes as exactly the shape of a menu whose first row is
    // selected. The phone then said a question was waiting and told the reader not to answer
    // from there, which left them no way to do anything.
    let typed = """
    \u{276F} 1. \u{4F60}\u{73FE}\u{5728}\u{662F}\u{5426}\u{8B93} web \u{7684}\u{8F38}\u{5165}bar
      2. \u{8F38}\u{5165} bar \u{53EF}\u{4EE5}\u{4E0D}\u{8981}\u{7B2C}\u{4E00}\u{500B}\u{5B57}
      3. \u{6211}\u{770B} web \u{7684}\u{8655}\u{7406}\u{72C0}\u{614B}
    """
    check("a list typed at the prompt is not a question", !SessionState.isChoosing(typed))
    // **An open gate is not enough on its own.** It means a hook said this session is waiting,
    // and auto mode raises permission events for approvals it then grants itself — so the gate
    // stands open while the screen shows whatever the session happens to be printing. Trusting
    // the caret on that alone put an echoed `\u{276F} 1. Yes` in front of somebody on a phone as a
    // real question, and they pressed it. A frame is the second fact required, and a list typed
    // at a prompt has none.
    check("nor does an open gate, with nothing framing it",
          !SessionState.isChoosing(typed, hookWaiting: true))
    expect("so the screen is not called waiting either",
           SessionState.read(typed, hookWaiting: true), .idle)
    check("a later composer still disqualifies the echoed list",
          !SessionState.isChoosing(typed + "\n\u{276F} ", hookWaiting: true))

    // What the gate is actually for: the same ambiguous caret, drawn inside a dialog.
    let framed = """
    \u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}
     \u{2610} web input

     Should the web input bar send straight away?

    \u{276F} 1. Yes, send it
      2. No, confirm first
      3. Show me the handling
    """
    check("a framed one with the gate open is a menu",
          SessionState.isChoosing(framed, hookWaiting: true))
    check("and the frame alone, with no gate, still is not",
          !SessionState.isChoosing(framed))
    guard let gated = SessionState.menu(framed, hookWaiting: true) else {
        check("the framed shape is a menu once a hook says waiting", false); return
    }
    expect("the hook-gated menu keeps every option", gated.options.count, 3)
    expect("the flush-left caret selects its numbered row", gated.selected, 1)
    expect("and its question comes from inside the frame",
           gated.question, "Should the web input bar send straight away?")

    // **The composer sits below the picker, and somebody has typed into it.** Claude Code does not
    // take the input line away while a question is up, so the moment a character is entered the
    // last caret on screen belongs to the composer — and keying on "the last caret" found that,
    // saw it was not a numbered row, and declared the whole screen not a menu. Captured from a
    // real session that was waiting on an answer while its reader was mid-sentence.
    let underComposer = """
    \u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}
    \u{2190}  \u{2610} scope  \u{2610} parity  \u{2714} Submit  \u{2192}

    \u{2502} Stop once more after the planner has chosen?

    \u{276F} 1. Only when it is unsure
      2. Every single time
      3. Never stop
    \u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}
      Chat about this

    Enter to select \u{00b7} \u{2191}/\u{2193} to navigate

      \u{276F} a sentence somebody is halfway through typing
    """
    guard let stillAMenu = SessionState.menu(underComposer, hookWaiting: true) else {
        check("a picker with a typed-in composer under it is still a menu", false); return
    }
    expect("the composer's caret does not hide the picker's", stillAMenu.options.count, 3)
    expect("and the numbered row keeps the selection", stillAMenu.selected, 1)
    expect("the question is still the dialog's",
           stillAMenu.question, "Stop once more after the planner has chosen?")

    // Real AskUserQuestion rows are separated by descriptions. Walking adjacent lines, like the
    // Codex parser does, stops at the first description; scanning every option row must not.
    let described = """
    \u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}
      1. Keep the current API
        This preserves existing callers.
    \u{276F} 2. Add the waiting gate
        This trusts the ambiguous caret only after a hook signal.
      3. Cancel
        Leave the code unchanged.
    """
    guard let describedMenu = SessionState.menu(described, hookWaiting: true) else {
        check("a described AskUserQuestion is a menu", false); return
    }
    expect("description lines do not stop option collection", describedMenu.options.count, 3)
    expect("described option numbers remain in screen order",
           describedMenu.options.map(\.number), [1, 2, 3])
    expect("the described selected row is correct", describedMenu.selected, 2)

    // And the thing it must still catch: a real dialog, drawn inside its box.
    let menu = """
    \u{2502} \u{276F} 1. Yes                  \u{2502}
    \u{2502}   2. Yes, and don't ask again  \u{2502}
    \u{2502}   3. No, tell Claude what to do\u{2502}
    """
    check("a dialog in its box still is", SessionState.isChoosing(menu))
    check("and remains one with the hook gate open",
          SessionState.isChoosing(menu, hookWaiting: true))

    // Indented but unboxed, which some prompts are.
    let bare = """
      \u{276F} 1. Keep going
        2. Stop here
    """
    check("and an indented one with no box", SessionState.isChoosing(bare))

    // The prompt on its own, which is every idle session on the machine.
    check("a bare prompt is not a menu", !SessionState.isChoosing("\u{276F} "))
}

group("rows with the session's own output under them are scrollback, not a dialog") {
    // **This reader did it to itself, 2026-08-26.** It pasted a picture of a picker into a message
    // so somebody could see the shape being discussed; the terminal drew the message; and every
    // guard in here passed on the drawing. An indented caret, a rule above it, numbered rows with
    // checkboxes and a `Submit` under them — so a session that was busy working was reported as
    // waiting, and a question nobody had asked went to a phone with buttons under it. Two of its
    // "options" were columns of unrelated prose that happened to line up.
    //
    // A dialog is a thing the session has stopped in front of. Nothing can be printed below it
    // until it is answered, so a turn marker underneath settles it.
    let quoted = """
    \u{23FA} 這一按把 bug 抓出來了。畫面現在是：

    \u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}
      \u{276F} 1. [ ] Tests
        2. [\u{2714}] Docs
        3. [ ] Refactor
           Submit

    \u{23FA} 確認了。j 不在輸入列放行的鍵裡，但 Tab 在。

    · Finagling… (2m 59s · \u{2193} 10.3k tokens)
    """
    check("a quoted picker with a turn under it is not a question",
          SessionState.menu(quoted, hookWaiting: true) == nil)
    expect("and the session reads as what it is actually doing",
           SessionState.read(quoted, hookWaiting: true),
           .working("Finagling… (2m 59s · \u{2193} 10.3k tokens)"))

    // The same rows with nothing under them are the dialog they look like. This is the half that
    // keeps the rule honest: it must reject what is quoted without rejecting what is drawn.
    let drawn = """
    \u{23FA} 這一按把 bug 抓出來了。畫面現在是：

    \u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}
      \u{276F} 1. [ ] Tests
        2. [\u{2714}] Docs
        3. [ ] Refactor
           Submit

    Enter to select · ↑/↓ to navigate · Esc to cancel
    """
    guard let live = SessionState.menu(drawn, hookWaiting: true) else {
        check("rows with only a hint under them are still a dialog", false); return
    }
    expect("with every row", live.options.count, 3)
    expect("and its button", live.submit?.label, "Submit")

    // A stale spinner **above** a dialog is an ordinary sight — Claude Code does not always erase
    // the line it was drawing when the question arrives — and the ordering in `read` exists for
    // exactly that. Only what sits below the rows is asked about.
    let staleAbove = """
    ✻ Generating… (9s)
    \u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}
      \u{276F} 1. Yes
        2. No
    """
    check("a spinner above the rows does not disqualify them",
          SessionState.isChoosing(staleAbove, hookWaiting: true))
}

group("a menu read as options a finger can hit") {
    // The shape a permission request actually arrives in: inside its box, one row carrying the
    // caret, the far wall of the dialog jammed against the longest label.
    let screen = """
    \u{2502} \u{276F} 1. Yes                          \u{2502}
    \u{2502}   2. Yes, and don't ask again           \u{2502}
    \u{2502}   3. No, tell Claude what to do instead \u{2502}
    """
    guard let menu = SessionState.menu(screen) else {
        check("a dialog is read as a menu", false); return
    }
    expect("every option is there", menu.options.count, 3)
    expect("the numbers are the ones printed", menu.options.map(\.number), [1, 2, 3])
    expect("the caret says which one is on screen", menu.selected, 1)

    // **The wall is not part of the answer.** Everything before the number is skipped on the way
    // in, and nothing used to be looking at the end — so the label arrived with a `\u{2502}` on it
    // and the phone drew a button with the side of a box in its name.
    expect("the box is not part of the label", menu.options[1].label, "Yes, and don't ask again")
    expect("nor of the last one",
           menu.options[2].label, "No, tell Claude what to do instead")

    // Every one of these is reachable by a keystroke, which is what makes it pressable.
    check("all three can be answered", menu.options.allSatisfy(\.answerable))

    // The two are the same reading. `isChoosing` exists because most callers only want the yes
    // or no, and a second parser would be a second thing to be wrong.
    check("and the old question is the same question", SessionState.isChoosing(screen))
}

group("a picker drawn without numbers") {
    // Claude Code has more than one shape of picker. Most are numbered — `1. Yes` — and the
    // number is both the evidence that a row is an option and the keystroke that answers it.
    // Some are not: the select component takes a `hideIndexes` flag, and when it is set the rows
    // are drawn as a pointer column, one gap, and the label. The held cross-session message below
    // is one of those, and so is the plan-approval dialog.
    //
    // Read from a real screen, 2026-08-26. Two rows, no numbers, the caret on the second.
    let held = """
     \u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}
      Held message from another session

      Another Claude session sent a message: from uds:/tmp/cc-socks/18772.sock

      The sending session's mode class does not match this one's, so it was not delivered.

        Deny \u{2014} drop it and tell the sender it was declined
      \u{276F} Deliver this message to Claude
    """
    guard let menu = SessionState.menu(held, hookWaiting: true) else {
        check("an unnumbered dialog is read as a menu", false); return
    }
    expect("both rows are options", menu.options.count, 2)
    expect("the labels are the rows themselves",
           menu.options.map(\.label),
           ["Deny \u{2014} drop it and tell the sender it was declined",
            "Deliver this message to Claude"])
    expect("the caret still says which row a bare Return would take", menu.selected, 2)
    check("and the rows are numbered by position so a finger can name one",
          menu.options.map(\.number) == [1, 2])
    check("but the menu says the numbers are not on screen", menu.numbered == false)
    check("a numbered dialog still says they are",
          SessionState.menu("\u{2502} \u{276F} 1. Yes \u{2502}\n\u{2502}   2. No \u{2502}")?.numbered == true)

    // **The evidence a number carries has to come from somewhere else.** A row with `1.` in front
    // of it is a thing prose does not write; a row that is only indented text is a thing prose
    // writes all day. So an unnumbered picker is admitted only when a hook says this session is
    // waiting *and* the rows sit under a frame — the same two facts the flush-left case needs.
    check("without the waiting gate it is just indented text",
          SessionState.menu(held, hookWaiting: false) == nil)

    let unframed = """
    Some output the assistant printed, and then a quoted prompt:

        second line of the quote
      \u{276F} first line of the quote
    """
    check("and without a frame above it, it is not a dialog",
          SessionState.menu(unframed, hookWaiting: true) == nil)

    // One row is not a choice. The same rule the numbered path has, for the same reason.
    let lonely = """
    \u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}
      \u{276F} the only line here
    """
    check("a single row is not a menu", SessionState.menu(lonely, hookWaiting: true) == nil)

    // A description belongs to the row above it, not beside it: the dialog indents it two
    // further columns, which is exactly what keeps it from being read as a third option.
    let described = """
    \u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}
      Ship the tested build now?

      \u{276F} Ship it
          the build that finished at noon
        Keep testing
    """
    guard let build = SessionState.menu(described, hookWaiting: true) else {
        check("a described unnumbered dialog is a menu", false); return
    }
    expect("its description is not a third row", build.options.count, 2)
    expect("the description is kept with its row",
           build.options[0].detail, "the build that finished at noon")
    expect("and the prose above is the question", build.question, "Ship the tested build now?")
}

group("an unnumbered picker is answered by walking the highlight, not by typing a digit") {
    // The same component that hides the numbers also turns numeric selection off — its key
    // handler tests `disableSelection !== "numeric"` before it looks at a digit at all. So a `3`
    // sent at one of these does not choose the third row; it falls through the dialog and lands
    // in the composer. What does move the highlight is `j` and `k`, which the Select context
    // binds beside the arrow keys, and `Return` still accepts.
    expect("moving down is j, once per row", Targets.walk(from: 1, to: 3).key, UInt8(0x6a))
    expect("as many times as there are rows between", Targets.walk(from: 1, to: 3).times, 2)
    expect("moving up is k", Targets.walk(from: 3, to: 1).key, UInt8(0x6b))
    expect("the same distance the other way", Targets.walk(from: 3, to: 1).times, 2)
    expect("and a row already under the caret needs no walking at all",
           Targets.walk(from: 2, to: 2).times, 0)

    // The door itself does not widen. `j` and `k` are produced in here the way the confirming
    // Return already is; nothing new reaches a tty from outside, and the allowlist above is
    // still the whole of what a phone may send.
    let session = TargetSession(backend: .tmux, id: "%nope%", name: "x", tty: "/dev/ttys99",
                                windowIndex: 0, tabIndex: 0, assistant: .claude)
    check("a phone still cannot send j",
          Targets.answer(0x6a, to: session) == "That is not a key this can send.")
    check("nor k", Targets.answer(0x6b, to: session) == "That is not a key this can send.")
}

group("a multi-select's button is walked to with Tab, because its last row eats anything else") {
    // Measured against a real dialog on 2026-08-26, and it is the reason this constant exists
    // rather than being written inline. `j` is bound to "next row" and was the obvious step; it
    // walked four rows and then typed `jjjj` into the question's own text box, because the last
    // row of an AskUserQuestion is `Type something` — an input — and a focused input passes only
    // `up`, `down`, `escape`, `tab` and `return` through, swallowing everything else as typing.
    expect("the step is Tab", Targets.submitStep, UInt8(0x09))
    check("and not the row-navigation key it looks like it should be",
          Targets.submitStep != 0x6a)

    // Tab was already the one non-digit key a phone may send, so walking with it widens nothing.
    let session = TargetSession(backend: .tmux, id: "%nope%", name: "x", tty: "/dev/ttys99",
                                windowIndex: 0, tabIndex: 0, assistant: .claude)
    check("so the allowlist did not have to change",
          Targets.answer(Targets.submitStep, to: session) != "That is not a key this can send.")
}

group("the Submit a multi-select puts under its rows is a row of its own") {
    // Captured from a real AskUserQuestion multi-select, 2026-08-26. Three things about the shape
    // are load-bearing and none of them were guessed: the options carry an ASCII `[ ]` rather than
    // a checkbox glyph, their labels start in column 5 while their descriptions start in column 2,
    // and **the submit button is drawn as an unnumbered line in the label's column**. That last
    // one is the bug — `detail(under:in:)` walks down from the last option until it meets
    // something that is not prose, and an unnumbered line is prose to it, so the button arrived on
    // the phone as the fifth option's description and there was nothing left to press.
    let multi = """
    \u{2190}  \u{2610} 多選修法  \u{2714} Submit  \u{2192}

    │ 多選這種形狀，你要的修法是哪一種？

    \u{276F} 1. [ ] Submit 變成可點的一列
      最小修法：把 Submit 當成獨立一列。
      2. [ ] 勾選狀態要看得出來
      把每列的方框解析成一個真正的欄位。
      3. [ ] 點一下就等於送出
      4. [ ] Type something
         Submit
    \u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}
      5. Chat about this

    Enter to select · ↑/↓ to navigate · Esc to cancel
    """
    guard let menu = SessionState.menu(multi, hookWaiting: true) else {
        check("a multi-select is a menu", false); return
    }
    expect("the numbered rows are the options", menu.options.count, 5)
    check("the button is not the last option's description",
          menu.options[3].detail == nil, "got \(String(describing: menu.options[3].detail))")
    expect("it is a row of its own", menu.submit?.label, "Submit")
    check("and it is not where the caret is", menu.submit?.selected == false)
    expect("the question is still the dialog's own",
           menu.question, "多選這種形狀，你要的修法是哪一種？")

    // Walked down onto it, the caret sits in the pointer's cell exactly as it does on an option.
    let onSubmit = multi.replacingOccurrences(of: "\u{276F} 1. [ ]", with: "  1. [ ]")
        .replacingOccurrences(of: "     Submit", with: "\u{276F}    Submit")
    check("the caret on the button is read as the caret on the button",
          SessionState.menu(onSubmit, hookWaiting: true)?.submit?.selected == true)

    // **A single-select has no button, and its descriptions sit in the label's column.** So a rule
    // that keyed on the column alone would turn the first description of every ordinary
    // AskUserQuestion into a Submit nobody can press. The checkbox is what says which dialog this
    // is, and it is required.
    let single = """
    \u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}
     \u{2610} build

    │ 別的 session 把 index.html 寫完了，還沒 commit。

    \u{276F} 1. 幫它整理並 commit，再 build
         跟先前那批一樣，先整理工作區。
      2. 直接 build，不碰它的 commit
    """
    guard let plain = SessionState.menu(single, hookWaiting: true) else {
        check("a single-select is still a menu", false); return
    }
    check("a single-select grows no button", plain.submit == nil)
    expect("and its description is still a description",
           plain.options[0].detail, "跟先前那批一樣，先整理工作區。")
}

group("a multi-select's box is a fact about the row, not two characters in front of it") {
    let multi = """
    \u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}
     \u{2610} scope

    │ 哪些要拔掉？

    \u{276F} 1. [ ] the rate limit
      2. [\u{2714}] the licence
      3. [ ] Type something
         Submit
    """
    guard let menu = SessionState.menu(multi, hookWaiting: true) else {
        check("that is a menu", false); return
    }
    expect("the box comes off the label", menu.options.map(\.label),
           ["the rate limit", "the licence", "Type something"])
    expect("and becomes what it always was", menu.options.map(\.checked), [false, true, false])

    // A dialog without boxes has rows that answer rather than tick, and saying `false` about them
    // would draw an empty box beside every option of every permission prompt.
    let plain = SessionState.menu("\u{2502} \u{276F} 1. Yes \u{2502}\n\u{2502}   2. No \u{2502}")
    expect("a row that does not tick says nothing about ticking",
           plain?.options.map(\.checked), [nil, nil])
}

group("a checkbox inside a description is text, not the header above the question") {
    // The tab bar `\u{2190}  \u{2610} scope  \u{2714} Submit  \u{2192}` is drawn above the
    // question, so a line carrying a checkbox glyph is a boundary when the prose above a menu is
    // read *upwards*. Read downwards it is nothing of the sort — a description that happens to
    // mention one was being dropped, which a real capture showed on 2026-08-26: the second row of
    // a multi-select lost its explanation because the explanation was about checkboxes.
    let numbered = """
    \u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}
     \u{2610} scope

    │ 哪一種修法？

    \u{276F} 1. 勾選狀態要看得出來
      把每列的 \u{2610}／\u{2611} 解析成一個真正的欄位。
      2. 不用管勾選
    """
    guard let menu = SessionState.menu(numbered, hookWaiting: true) else {
        check("that dialog is a menu", false); return
    }
    expect("a description that mentions a checkbox survives",
           menu.options[0].detail, "把每列的 \u{2610}／\u{2611} 解析成一個真正的欄位。")
    expect("and the header above is still not the question", menu.question, "哪一種修法？")

    // The same walk, on a picker that prints no numbers.
    let plain = """
    \u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}\u{2500}
      哪一種修法？

      \u{276F} 勾選狀態要看得出來
          把每列的 \u{2610}／\u{2611} 解析成一個真正的欄位。
        不用管勾選
    """
    expect("an unnumbered row keeps it too",
           SessionState.menu(plain, hookWaiting: true)?.options[0].detail,
           "把每列的 \u{2610}／\u{2611} 解析成一個真正的欄位。")
}

group("the words under an option come from the transcript, which has all of them") {
    // A terminal lays the dialog out to fit the window and squeezes the paragraph under each
    // option into whatever height is left, so a capture carries the first line with the middle
    // cut out of it. Measured on 2026-08-26: `…as little of the existing ` ran straight into
    // `scoped session.` on screen while the transcript held all 254 characters.
    //
    // These two rows are the shape that file has. The call is the last thing in it while the
    // picker waits; the result lands behind it the moment somebody answers.
    let call = """
    {"type":"assistant","timestamp":"2026-08-26T13:55:54.405Z","message":{"role":"assistant",\
    "content":[{"type":"tool_use","id":"toolu_x","name":"AskUserQuestion","input":{"questions":\
    [{"question":"Which way out?","header":"scope","options":\
    [{"label":"Swap the feed","description":"Three of the eleven bindings are primary."},\
    {"label":"Pay for the tier","description":"Nothing in the code changes."}]}]}}]}}
    """
    let answer = """
    {"type":"user","timestamp":"2026-08-26T13:56:21.893Z","message":{"role":"user",\
    "content":[{"type":"tool_result","tool_use_id":"toolu_x","content":"answered"}]}}
    """

    guard let open = Transcript.openQuestion(inTail: call) else {
        check("an unanswered call is an open question", false); return
    }
    expect("with the question it asked", open.text, "Which way out?")
    expect("and both rows", open.options.count, 2)
    expect("carrying the words the screen had no room for",
           open.options[0].note, "Three of the eleven bindings are primary.")

    check("once the answer lands it is not open any more",
          Transcript.openQuestion(inTail: call + "\n" + answer) == nil)
    check("and an ordinary transcript has no open question",
          Transcript.openQuestion(inTail: answer) == nil)

    // The rows reach the phone as a menu, and the note has to travel with them — dropping it was
    // the whole bug: the buttons arrived with their headlines and none of the reasoning.
    let menu = open.menu
    expect("the note becomes the row's detail", menu?.options[0].detail,
           "Three of the eleven bindings are primary.")
    expect("for every row", menu?.options[1].detail, "Nothing in the code changes.")
}

group("the question above a visual menu") {
    // AskUserQuestion's descriptions belong to their options, not to the prose above the first
    // one. The short checkbox line is only a header, so the phone gets the two wrapped question
    // lines and not that classification label.
    let described = """
    ────────────────────────────────────────────
     ☐ build
     別的 session 把 index.html 寫完了，但還沒 commit。
     root 連坐的修正要生效就得 build。怎麼走？
    ❯ 1. 幫它整理並 commit，再 build（推薦）
         跟先前那批一樣，先整理工作區。
      2. 直接 build，不碰它的 commit
         保留目前的提交狀態。
      3. 先不要 build
    ────────────────────────────────────────────
    """
    guard let describedMenu = SessionState.menu(described, hookWaiting: true) else {
        check("a described question is still a menu", false); return
    }
    expect("wrapped question lines are joined",
           describedMenu.question,
           "別的 session 把 index.html 寫完了，但還沒 commit。 root 連坐的修正要生效就得 build。怎麼走？")
    expect("description rows do not become options", describedMenu.options.count, 3)

    // **As the terminal actually draws it.** The dialog puts a blank row between the header and
    // the question and another between the question and the first option. The fixture above has
    // none, because it was written as adjacent lines — and that difference was the whole bug: the
    // first blank read as the top of the prose, so a real dialog yielded no question at all while
    // this group stayed green. Padding is stepped over now, and this is the shape that proves it.
    let padded = """
    ────────────────────────────────────────────
     ☐ build

    │ 別的 session 把 index.html 寫完了，但還沒 commit。

    ❯ 1. 幫它整理並 commit，再 build（推薦）
         跟先前那批一樣，先整理工作區。
      2. 直接 build，不碰它的 commit
      3. 先不要 build
    ────────────────────────────────────────────
    """
    expect("padding inside the dialog is not the top of the question",
           SessionState.menu(padded, hookWaiting: true)?.question,
           "別的 session 把 index.html 寫完了，但還沒 commit。")

    // Prose that no edge closed off is not the question. Every dialog that has actually been
    // captured is framed; without a rule, a header or a caret above it there is nothing to say
    // where the question starts, and guessing put a page of someone's analysis on a phone.
    let unframed = """
    This belongs to the conversation, not to the dialog.


     Ship the tested build now?
    ❯ 1. Yes
      2. No
    """
    check("prose with no edge above it is not read as the question",
          SessionState.menu(unframed, hookWaiting: true)?.question == nil)

    // **Numbered rows above the frame belong to whoever wrote them.** An assistant listing three
    // findings, then a dialog with three options, was read as one menu of eight — and because the
    // first of those eight sat outside the dialog, the question was taken from the prose above
    // *that*. Both halves are asserted here: the count, and where the question came from.
    let listAbove = """
      三個順手挖到的東西

      1. 旗標是我們自己的 prompt 教出來的。
      2. zh_script.py 把「制」列為簡體字，那是錯的。
      3. 離線 dump 的 rewarm 通道灌的是同一段摘要。

      Opus 基準 20 題已完成 16 題。
    ────────────────────────────────────────────
    ←  ☐ 全跑範圍  ☐ digit 對等  ✔ Submit  →

    要怎麼跑完整那一輪？

    ❯ 1. 砍掉慢的五個，跑剩 17 題（推薦）
         淘汰最慢的兩個候選。
      2. 全部也跑完
      3. 先把題庫拉到 40 題再跑
    """
    guard let bounded = SessionState.menu(listAbove, hookWaiting: true) else {
        check("a dialog under a written list is still a menu", false); return
    }
    expect("rows above the frame are not options", bounded.options.count, 3)
    expect("and the question is the dialog's own", bounded.question, "要怎麼跑完整那一輪？")

    let oneLine = """
    ╭──────────────────────────╮
    │ Do you want to proceed?  │
    │ ❯ 1. Yes                 │
    │   2. No                  │
    ╰──────────────────────────╯
    """
    expect("a one-line permission question is read",
           SessionState.menu(oneLine)?.question, "Do you want to proceed?")

    let conversationAbove = """
    This sentence belongs to the earlier conversation.
    So does this one.
    ────────────────────────────────────────────
     ☐ deploy
     Ship the tested build now?
    ❯ 1. Ship it
      2. Keep testing
    """
    expect("a dialog rule keeps earlier conversation out",
           SessionState.menu(conversationAbove, hookWaiting: true)?.question,
           "Ship the tested build now?")

    let noQuestion = """
    │ ❯ 1. Yes │
    │   2. No  │
    """
    let fallback = SessionState.menu(noQuestion)
    check("a menu with no readable question keeps nil", fallback?.question == nil)
    expect("missing prose does not lose its options", fallback?.options.count, 2)
}

group("a menu row no keystroke can reach is shown and not offered") {
    // Ten options is not a shape Claude Code draws today, and the failure if it ever does must
    // not be a button that answers a different question: `Targets.answer` carries 1...9, so the
    // tenth row is drawn and refused rather than quietly renumbered.
    var rows = "\u{2502} \u{276F} 1. first  \u{2502}\n"
    for n in 2...10 { rows += "\u{2502}   \(n). option \(n) \u{2502}\n" }
    guard let menu = SessionState.menu(rows, tailLines: 40) else {
        check("ten options is still a menu", false); return
    }
    expect("all ten are read", menu.options.count, 10)
    check("the first nine can be answered",
          menu.options.prefix(9).allSatisfy(\.answerable))
    check("and the tenth cannot", !menu.options[9].answerable)
}

group("what a background agent left on disk") {
    // The one record that says an agent ended. There is nothing that says one *started* — see
    // `Subagents` — so this is the whole of how "still running" is decided: by its absence.
    let folder = URL(fileURLWithPath: NSTemporaryDirectory())
        .appendingPathComponent("clawdline-agents-\(UUID().uuidString)", isDirectory: true)
    try? FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: folder) }

    let note = "<task-notification>\\n<task-id>a42cc4cf998a3ae33</task-id>\\n"
        + "<status>completed</status>\\n<summary>Agent \\\"Probe\\\" finished</summary>\\n"
        + "<result>52 files, and the three markers ran in order.</result>\\n"
        + "<usage><subagent_tokens>23771</subagent_tokens><tool_uses>7</tool_uses>"
        + "<duration_ms>79946</duration_ms></usage>\\n</task-notification>"

    let transcript = folder.appendingPathComponent("session.jsonl")
    let lines = [
        #"{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"off it goes"}]}}"#,
        "{\"type\":\"queue-operation\",\"operation\":\"enqueue\",\"content\":\"\(note)\"}",
    ].joined(separator: "\n") + "\n"
    try? lines.write(to: transcript, atomically: true, encoding: .utf8)

    let found = Subagents.notices(in: transcript)
    expect("the ending is found", found["a42cc4cf998a3ae33"]?.status, "completed")
    expect("with what it came back with",
           found["a42cc4cf998a3ae33"]?.result, "52 files, and the three markers ran in order.")
    expect("and what it cost", found["a42cc4cf998a3ae33"]?.tokens, 23771)
    expect("in tool calls", found["a42cc4cf998a3ae33"]?.tools, 7)

    // **Read forward, not from the end**, so a second look does not re-read the file and does
    // not lose what the first one learned. Appending a line nothing cares about must leave the
    // verdict exactly where it was.
    if let handle = try? FileHandle(forWritingTo: transcript) {
        _ = try? handle.seekToEnd()
        try? handle.write(contentsOf: Data(#"{"type":"user","message":{"role":"user","content":"ok"}}"# .utf8 + Data("\n".utf8)))
        try? handle.close()
    }
    let again = Subagents.notices(in: transcript)
    expect("a later read keeps what the first one found", again["a42cc4cf998a3ae33"]?.status,
           "completed")

    // A transcript with no endings in it leaves every agent it mentions running, which is the
    // state this is all in aid of.
    let quiet = folder.appendingPathComponent("quiet.jsonl")
    try? #"{"type":"user","message":{"role":"user","content":"hello"}}"# .write(to: quiet,
                                                                               atomically: true,
                                                                               encoding: .utf8)
    check("a transcript with no endings has no verdicts", Subagents.notices(in: quiet).isEmpty)

    // And the file that is not there at all, which is the ordinary state for a session that has
    // never sent anything off.
    check("a missing transcript is not an error",
          Subagents.notices(in: folder.appendingPathComponent("nope.jsonl")).isEmpty)
}

group("what a background agent is doing right now") {
    let folder = URL(fileURLWithPath: NSTemporaryDirectory())
        .appendingPathComponent("clawdline-doing-\(UUID().uuidString)", isDirectory: true)
    try? FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: folder) }

    // The **last** tool it reached for, not the first: this is the closest thing to a live line
    // that something without a screen has.
    let jsonl = folder.appendingPathComponent("agent-a1.jsonl")
    let rows = [
        #"{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Read","input":{"file_path":"Sources/Panel.swift"}}]}}"#,
        #"{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"…"}]}}"#,
        #"{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"command":"swift build","description":"Build"}}]}}"#,
    ].joined(separator: "\n") + "\n"
    try? rows.write(to: jsonl, atomically: true, encoding: .utf8)

    expect("the newest tool call wins", Subagents.doing(in: jsonl), "Bash: swift build")

    // A transcript that has not reached for anything yet says nothing rather than guessing.
    let fresh = folder.appendingPathComponent("agent-a2.jsonl")
    try? #"{"type":"user","message":{"role":"user","content":"go"}}"# .write(to: fresh,
                                                                            atomically: true,
                                                                            encoding: .utf8)
    check("an agent that has not used a tool says nothing", Subagents.doing(in: fresh) == nil)
}

group("following an agent to its own transcript") {
    // The id in `/v1/sessions/<id>/agents/<agentId>` and in the pane's own tabs is about to
    // become a path component, so it is checked rather than trusted. Everything Claude Code
    // writes is hex; everything that could leave the directory is not.
    check("an ordinary id is one", Subagents.isID("a44b12139eff09dd4"))
    check("and so is a dashed one", Subagents.isID("a42cc4cf-998a-3ae3"))
    check("nothing is not", !Subagents.isID(""))
    check("a parent directory is refused", !Subagents.isID(".."))
    check("a path is refused", !Subagents.isID("../../../etc/passwd"))
    check("a dot is enough to refuse", !Subagents.isID("agent.jsonl"))
    check("a separator is refused", !Subagents.isID("a1/a2"))
    check("and a name nobody could have written is refused",
          !Subagents.isID(String(repeating: "a", count: 200)))

    // **Every row in an agent's file is marked as a sidechain** — that is what an agent is, from
    // the session's point of view — and a sidechain is exactly what the session's own transcript
    // drops. Read with the session's rule, a busy agent reads as one that has written nothing.
    let row = #"{"type":"assistant","isSidechain":true,"message":{"role":"assistant","content":[{"type":"text","text":"had a look, nothing in the logs"}]}}"#
    expect("a session's transcript drops sidechains",
           Transcript.parse(row, assistant: .claude).count, 0)
    let asAgent = Transcript.parse(row, assistant: .claude, sidechains: true)
    expect("an agent's own transcript is nothing else", asAgent.count, 1)
    expect("and it reads as what the agent said",
           asAgent.first?.text, "had a look, nothing in the logs")
}
}
