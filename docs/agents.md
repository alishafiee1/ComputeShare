# Using a ComputeShare node from AI tools and bots

A node is a Linux computer you reach with ordinary SSH. Any tool that can run SSH can use it: an editor
(Cursor, VS Code), a coding agent (Claude Code, Grok bot) or your own script. Nothing special is installed on
your side except `cs`, which writes the SSH settings for you.

## 1. One-time setup

```bash
cs setup                  # or, from the node owner:  cs add myBrotherPc SHA256:<fingerprint>
ssh myBrotherPc.cs        # you are inside the node's isolated Linux space
```

The owner of the node gives you either a password or adds your public key to the space user. Check the
node's fingerprint with the owner once (`cs add` does this for you); after that a wrong server is refused.
To see who is online without logging in: `ssh list@node.n3xus.ir -p 2222 "list json"`.

## 2. Editors and agents

| Tool | How it connects |
|---|---|
| Any terminal or script | `ssh myBrotherPc.cs`, `scp file myBrotherPc.cs:/work/in/` |
| Cursor / VS Code (Remote-SSH) | Remote-SSH: connect to host `myBrotherPc.cs` (the entry is already in `~/.ssh/config` after `cs setup`) |
| Claude Code, Grok bot, other coding agents | give the agent a shell that can run `ssh myBrotherPc.cs <command>`; the agent works inside the space, never on the owner's computer |
| Python scripts | `asyncssh` with the jump; see `examples/bot/` |

## 3. Files and a GPU

- Put work in `/work/in`, read results from `/work/out`. The space has a GPU only if the owner shared it
  (`nvidia-smi` shows it).
- Reach a model server inside the space as if it were local: `ssh -N -L 11434:localhost:11434 myBrotherPc.cs`, then call
  `http://localhost:11434/v1/chat/completions` (OpenAI-compatible).

## 4. Rules that protect everyone

- The space cannot reach the owner's files, network or computer. The owner can pause sharing or kick you at any time.
- Keep tokens and keys in environment variables, never in files in a repository.
- Do not stress the shared limits: the CPU, memory and disk shown by `ssh list@...` are what the owner shares.
- The server in the middle only routes the connection. It cannot read what you send.
