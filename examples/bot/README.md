# Voice-to-text bot on a friend's GPU

A Telegram or Bale bot that sends each voice message to a friend's computer (a ComputeShare node), runs
Whisper there on the GPU and replies with the text. The friend's files and network are never touched: the
bot only enters the isolated Linux space of the node.

## What you need

1. A node with a GPU shared into its space (`csnode`), with `whisper` installed in the space, and your bot's
   SSH key added to the space user's `authorized_keys` by the node owner.
2. On the bot's computer: `cs setup` (or the owner's line `cs add myBrotherPc SHA256:...`) so that
   `~/.ssh/cs_known_hosts` holds the node's host key. The bot refuses a node whose key does not match.
3. `pip install -r requirements.txt` (Python 3.11+).

## Run

```bash
export BOT_TOKEN=...            # from BotFather (Telegram) or Bale; the only place a token may live
export CS_NODE=myBrotherPc CS_KEY=~/.ssh/cs_bot_ed25519
python voice_bot.py             # Bale: also export BOT_API=https://tapi.bale.ai
```

Settings (all optional except `BOT_TOKEN`): `CS_SERVER` (default `node.n3xus.ir`), `CS_PORT` (2222), `CS_NODE`,
`CS_USER` (`user`), `CS_KEY`, `CS_KNOWN_HOSTS`, `WHISPER_MODEL` (`small`), `BOT_API`.

## What happens to one voice message

1. `list json` from the broker: if the node is not `online`, the bot answers `پردازشگر خاموشه، بعداً دوباره بفرست`.
2. SFTP upload to `/work/in/<random>.ogg`, `whisper --device cuda` writes `/work/out/<random>.txt`, the bot downloads it.
3. Both remote files are deleted. The log shows the GPU name and the seconds per file.
4. Without a GPU in the space the bot falls back to the CPU and says so. Files over 100 MB are refused.

## Use the model server as if it were local

`python model_api_demo.py` forwards local port 11434 to port 11434 inside the space (like
`ssh -N -L 11434:localhost:11434 myBrotherPc.cs`) and sends an OpenAI-compatible request to
`http://localhost:11434/v1/chat/completions`.

## Test without a network

```bash
python -m unittest -v
```

## Safety notes

- The broker only routes. The inner SSH connection is end to end; its host key is checked against `cs_known_hosts`.
- The token is read from `BOT_TOKEN` only, never from a file; the tests fail if a token or key is committed in this folder.
- Real end-to-end result (voice to text in under 20 s on an RTX-class GPU) is still to be recorded on a real node; see umbrella card 011.
