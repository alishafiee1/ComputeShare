"""Use the model server of a friend's computer as if it were local (card 011 R4).

Same as:  ssh -N -L 11434:localhost:11434 myBrotherPc.cs
and then an OpenAI-compatible call to http://localhost:11434/v1/chat/completions
"""
from __future__ import annotations

import asyncio
import json
import urllib.request

import csbot


def chat(prompt: str, model: str = "hermes3:8b", port: int = 11434) -> str:
    body = json.dumps({"model": model, "messages": [{"role": "user", "content": prompt}]}).encode()
    req = urllib.request.Request(f"http://localhost:{port}/v1/chat/completions", body, {"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=120) as r:  # noqa: S310 - local forward
        return json.load(r)["choices"][0]["message"]["content"]


async def main() -> None:
    s = csbot.Settings.from_env()
    jump, conn = await csbot.connect(s)
    try:
        # a local port that leads to port 11434 inside the space, through the encrypted SSH connection
        listener = await conn.forward_local_port("127.0.0.1", 11434, "localhost", 11434)
        print(await asyncio.to_thread(chat, "سلام! یک جملهٔ کوتاه بگو."))
        listener.close()
    finally:
        conn.close()
        jump.close()


if __name__ == "__main__":
    asyncio.run(main())
