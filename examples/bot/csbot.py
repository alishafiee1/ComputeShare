"""ComputeShare example bot: send a voice message to a friend's GPU computer, get the text back.

The pieces here have no network code of their own except `asyncssh`, imported only when a connection
is opened, so the logic can be tested without it (see test_csbot.py).

How it reaches the node (umbrella card 011):
  bot --SSH--> broker "join" jump (:2222) --direct-tcpip--> <node>:22 --E2E SSH--> the space
The jump is only a router: the broker cannot read the inner session. So the *inner* host key is the one
that matters and is checked against ~/.ssh/cs_known_hosts (written by `cs setup` or `cs add`).
"""
from __future__ import annotations

import json
import os
import shlex
import time
import uuid
from dataclasses import dataclass
from pathlib import Path

OFFLINE_REPLY = "پردازشگر خاموشه، بعداً دوباره بفرست"
TOO_BIG_REPLY = "فایل بزرگ‌تر از ۱۰۰ مگابایت است؛ کوتاه‌ترش کن"
CPU_NOTICE = "پردازش روی CPU است و کندتر جواب می‌دهد"
MAX_BYTES = 100 * 1024 * 1024  # card 011: files over 100 MB are refused with a message
REMOTE_IN, REMOTE_OUT = "/work/in", "/work/out"


@dataclass
class Settings:
    server: str = "node.n3xus.ir"      # broker host
    port: int = 2222                   # the only SSH port of the broker
    node: str = "myBrotherPc"          # the node name
    user: str = "user"                 # the space user
    key: str = ""                      # path to the private key that the node owner authorised
    known_hosts: str = ""              # default: ~/.ssh/cs_known_hosts
    model: str = "small"               # whisper model

    @classmethod
    def from_env(cls) -> "Settings":
        e = os.environ
        return cls(
            server=e.get("CS_SERVER", cls.server), port=int(e.get("CS_PORT", cls.port)),
            node=e.get("CS_NODE", cls.node), user=e.get("CS_USER", cls.user),
            key=e.get("CS_KEY", ""), known_hosts=e.get("CS_KNOWN_HOSTS", ""), model=e.get("WHISPER_MODEL", cls.model),
        )

    def known_hosts_path(self) -> str:
        return self.known_hosts or str(Path.home() / ".ssh" / "cs_known_hosts")


def bot_token(name: str = "BOT_TOKEN") -> str:
    """The token comes from the environment only (card 011 R6); a missing token is an error, never a default."""
    t = os.environ.get(name, "").strip()
    if not t:
        raise RuntimeError(f"set the environment variable {name} (the bot token is never read from a file)")
    return t


def node_status(list_json: str, node: str) -> str:
    """Status of `node` in the output of `list json` ("online", "paused", "offline", ... or "unknown")."""
    try:
        rows = json.loads(list_json)
    except json.JSONDecodeError:
        return "unknown"
    for r in rows if isinstance(rows, list) else []:
        if str(r.get("name", "")).lower() == node.lower():
            return str(r.get("status", "unknown"))
    return "unknown"


def is_online(list_json: str, node: str) -> bool:
    return node_status(list_json, node) == "online"


def remote_names(ext: str = "ogg") -> tuple[str, str, str]:
    """(input path, output text path, id) for one job; the id is random so jobs never collide."""
    jid = uuid.uuid4().hex
    return f"{REMOTE_IN}/{jid}.{ext}", f"{REMOTE_OUT}/{jid}.txt", jid


def whisper_command(remote_in: str, model: str = "small", gpu: bool = True) -> str:
    """The shell command run in the space (card 011 R3, R7: the GPU is used when there is one)."""
    dev = "cuda" if gpu else "cpu"
    return (f"whisper {shlex.quote(remote_in)} --model {shlex.quote(model)} --device {dev} "
            f"--output_dir {shlex.quote(REMOTE_OUT)} --output_format txt --fp16 {'True' if gpu else 'False'}")


def gpu_line(nvidia_smi_output: str) -> str:
    """First GPU name from `nvidia-smi --query-gpu=name --format=csv,noheader`, or "" when there is none."""
    for line in nvidia_smi_output.splitlines():
        if line.strip():
            return line.strip()
    return ""


def check_size(n: int) -> str:
    """"" when the file is acceptable, otherwise the reply to send."""
    return TOO_BIG_REPLY if n > MAX_BYTES else ""


@dataclass
class Result:
    text: str
    gpu: str       # GPU name, "" when the CPU was used
    seconds: float


async def connect(s: Settings):
    """Open the jump and the inner SSH connection. Returns (jump, conn); close both when done."""
    import asyncssh  # imported here so the helpers above work without it

    # The broker's own host key is not pinned: it only routes. The inner host key is pinned below.
    jump = await asyncssh.connect(s.server, port=s.port, username="join", known_hosts=None, client_keys=None, password=None)
    conn = await asyncssh.connect(
        s.node.lower(), port=22, tunnel=jump, username=s.user,
        client_keys=[s.key] if s.key else None, known_hosts=s.known_hosts_path())
    return jump, conn


async def list_json(s: Settings) -> str:
    """`list json` from the broker (card 011 R2): who is online, without logging in as anyone."""
    import asyncssh

    async with asyncssh.connect(s.server, port=s.port, username="list", known_hosts=None, client_keys=None, password=None) as c:
        r = await c.run("list json", check=False)
        return str(r.stdout)


async def transcribe(s: Settings, local_file: str) -> Result:
    """Upload, run whisper on the node's GPU, download the text, delete the remote files."""
    size = os.path.getsize(local_file)
    if msg := check_size(size):
        raise ValueError(msg)
    r_in, r_out, _ = remote_names(Path(local_file).suffix.lstrip(".") or "ogg")
    t0 = time.monotonic()
    jump, conn = await connect(s)
    try:
        gpu = gpu_line(str((await conn.run("nvidia-smi --query-gpu=name --format=csv,noheader", check=False)).stdout))
        await conn.run(f"mkdir -p {REMOTE_IN} {REMOTE_OUT}", check=True)
        async with conn.start_sftp_client() as sftp:
            await sftp.put(local_file, r_in)
            done = await conn.run(whisper_command(r_in, s.model, gpu=bool(gpu)), check=False)
            if done.exit_status != 0:
                raise RuntimeError(f"whisper failed: {str(done.stderr)[-300:]}")
            async with sftp.open(r_out) as f:
                text = (await f.read()).strip()
            for p in (r_in, r_out):
                await sftp.remove(p)
        return Result(text=text if isinstance(text, str) else text.decode("utf-8", "replace"), gpu=gpu, seconds=time.monotonic() - t0)
    finally:
        conn.close()
        jump.close()
