"""Clawdline's fixed entry point: local AST only, without project execution.

The audit hook is a Python network/process tripwire, not an OS sandbox for
native libraries. Only the hash-pinned parser wheels are loaded as native code.
"""
import sys
import os
import threading


def deny_external(event, args):
    if event.startswith("socket.") or event in {
        "subprocess.Popen", "os.system", "os.posix_spawn", "os.exec", "os.fork",
        "os.forkpty", "pty.spawn",
    }:
        raise PermissionError("Clawdline local parser blocks network and child processes")


sys.addaudithook(deny_external)


def watch_parent():
    # The daemon alone holds the pipe's write end. A lost parent cannot leave
    # this AST worker running indefinitely. Native code can delay a Python
    # thread, so this is a crash tripwire rather than an OS process sandbox.
    while os.read(0, 1):
        pass
    os._exit(74)


threading.Thread(target=watch_parent, daemon=True).start()

# Force sequential extraction before the CLI imports its AST entry point.
# --max-workers alone caps a pool but still tries to start a subprocess.
import graphify.extract

original_extract = graphify.extract.extract
parser_workers = int(sys.argv[3])


def sequential_extract(*args, **kwargs):
    kwargs["parallel"] = False
    kwargs["max_workers"] = parser_workers
    return original_extract(*args, **kwargs)


graphify.extract.extract = sequential_extract
from graphify.__main__ import main

sys.argv = ["graphify", "extract", sys.argv[1], "--code-only", "--no-gitignore",
            "--max-workers", str(parser_workers), "--out", sys.argv[2]]
main()
