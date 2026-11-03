"""Telegram / Bale voice-to-text bot that uses a friend's GPU through ComputeShare.

Both messengers speak the same Bot API, so one file serves both:
    BOT_TOKEN=... python voice_bot.py            # Telegram (default)
    BOT_TOKEN=... BOT_API=https://tapi.bale.ai python voice_bot.py   # Bale
Connection settings come from environment variables (see README.md). The token only comes from BOT_TOKEN.
"""
from __future__ import annotations

import asyncio
import json
import os
import tempfile
import urllib.parse
import urllib.request

import csbot

API = os.environ.get("BOT_API", "https://api.telegram.org").rstrip("/")


def call(token: str, method: str, params: dict | None = None, timeout: int = 60) -> dict:
    """One Bot API call (GET with a query string is accepted by both Telegram and Bale)."""
    url = f"{API}/bot{token}/{method}"
    if params:
        url += "?" + urllib.parse.urlencode(params)
    with urllib.request.urlopen(url, timeout=timeout) as r:  # noqa: S310 - fixed https base URL
        return json.load(r)


def download(token: str, file_id: str, dest: str) -> int:
    info = call(token, "getFile", {"file_id": file_id})["result"]
    with urllib.request.urlopen(f"{API}/file/bot{token}/{info['file_path']}", timeout=60) as r, open(dest, "wb") as f:  # noqa: S310
        data = r.read()
        f.write(data)
    return len(data)


def reply(token: str, chat_id: int, text: str) -> None:
    call(token, "sendMessage", {"chat_id": chat_id, "text": text})


async def handle(token: str, s: csbot.Settings, msg: dict) -> None:
    chat = msg["chat"]["id"]
    media = msg.get("voice") or msg.get("audio")
    if not media:
        reply(token, chat, "یک پیام صوتی بفرست")
        return
    if big := csbot.check_size(int(media.get("file_size", 0))):
        reply(token, chat, big)
        return
    try:
        if not csbot.is_online(await csbot.list_json(s), s.node):  # card 011 R2
            reply(token, chat, csbot.OFFLINE_REPLY)
            return
        with tempfile.TemporaryDirectory() as d:
            path = os.path.join(d, "in.ogg")
            await asyncio.to_thread(download, token, media["file_id"], path)
            res = await csbot.transcribe(s, path)
    except ValueError as e:  # size check inside transcribe
        reply(token, chat, str(e))
        return
    except Exception as e:  # network or node problem: say it plainly, never print the token
        print("job failed:", type(e).__name__, e)
        reply(token, chat, csbot.OFFLINE_REPLY)
        return
    note = "" if res.gpu else "\n" + csbot.CPU_NOTICE
    print(f"done in {res.seconds:.1f}s on {res.gpu or 'CPU'}")  # card 011 R7: GPU name and time per file
    reply(token, chat, (res.text or "(بدون متن)") + note)


async def main() -> None:
    token = csbot.bot_token()
    s = csbot.Settings.from_env()
    offset = 0
    print(f"bot is up; node {s.node} via {s.server}:{s.port}")
    while True:
        try:
            updates = await asyncio.to_thread(call, token, "getUpdates", {"timeout": 30, "offset": offset}, 40)
        except Exception as e:  # keep running through network blips
            print("poll error:", type(e).__name__)
            await asyncio.sleep(5)
            continue
        for u in updates.get("result", []):
            offset = u["update_id"] + 1
            if "message" in u:
                asyncio.create_task(handle(token, s, u["message"]))


if __name__ == "__main__":
    asyncio.run(main())
