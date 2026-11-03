"""Tests for the pure parts of the bot (no network, no asyncssh needed): python -m unittest -v"""
import json
import os
import pathlib
import re
import unittest

import csbot

ROWS = json.dumps([{"name": "myBrotherPc", "status": "online"}, {"name": "officeBox", "status": "offline"}])


class NodeStatus(unittest.TestCase):
    def test_online_and_offline(self):  # R2
        self.assertTrue(csbot.is_online(ROWS, "myBrotherPc"))
        self.assertTrue(csbot.is_online(ROWS, "mybrotherpc"))  # names are case-insensitive
        self.assertFalse(csbot.is_online(ROWS, "officeBox"))
        self.assertEqual(csbot.node_status(ROWS, "ghost"), "unknown")

    def test_bad_json_is_not_online(self):
        self.assertFalse(csbot.is_online("not json", "myBrotherPc"))
        self.assertFalse(csbot.is_online("{}", "myBrotherPc"))

    def test_offline_reply_text(self):
        self.assertEqual(csbot.OFFLINE_REPLY, "پردازشگر خاموشه، بعداً دوباره بفرست")


class RemoteJob(unittest.TestCase):
    def test_names_are_unique_and_under_work(self):  # R3
        a, b = csbot.remote_names(), csbot.remote_names()
        self.assertNotEqual(a[2], b[2])
        self.assertTrue(a[0].startswith("/work/in/") and a[0].endswith(".ogg"))
        self.assertTrue(a[1].startswith("/work/out/") and a[1].endswith(".txt"))
        self.assertEqual(os.path.basename(a[0]).split(".")[0], os.path.basename(a[1]).split(".")[0])

    def test_whisper_command_uses_the_gpu(self):  # R7
        cmd = csbot.whisper_command("/work/in/x.ogg", "small", gpu=True)
        self.assertIn("--device cuda", cmd)
        self.assertIn("--output_dir /work/out", cmd)
        self.assertIn("--device cpu", csbot.whisper_command("/work/in/x.ogg", gpu=False))

    def test_whisper_command_is_quoted(self):
        cmd = csbot.whisper_command("/work/in/a b;rm -rf x.ogg")
        self.assertIn("'/work/in/a b;rm -rf x.ogg'", cmd)

    def test_gpu_line(self):
        self.assertEqual(csbot.gpu_line("NVIDIA GeForce GTX 1660 Ti\n"), "NVIDIA GeForce GTX 1660 Ti")
        self.assertEqual(csbot.gpu_line(""), "")
        self.assertEqual(csbot.gpu_line("\n  \n"), "")


class Limits(unittest.TestCase):
    def test_size_limit(self):  # edge case: files over 100 MB are refused
        self.assertEqual(csbot.check_size(csbot.MAX_BYTES), "")
        self.assertEqual(csbot.check_size(csbot.MAX_BYTES + 1), csbot.TOO_BIG_REPLY)


class Secrets(unittest.TestCase):
    def test_token_only_from_environment(self):  # R6
        os.environ.pop("BOT_TOKEN", None)
        with self.assertRaises(RuntimeError):
            csbot.bot_token()
        os.environ["BOT_TOKEN"] = "123:abc"
        try:
            self.assertEqual(csbot.bot_token(), "123:abc")
        finally:
            del os.environ["BOT_TOKEN"]

    def test_no_token_or_key_committed_in_this_folder(self):
        here = pathlib.Path(__file__).parent
        bad = re.compile(r"\b\d{6,}:[A-Za-z0-9_-]{30,}\b|BEGIN [A-Z ]*PRIVATE KEY")
        for p in here.iterdir():
            if p.is_file() and p.suffix in {".py", ".md", ".txt"}:
                self.assertIsNone(bad.search(p.read_text(encoding="utf-8")), p.name)

    def test_settings_default_known_hosts(self):
        self.assertTrue(csbot.Settings().known_hosts_path().endswith("cs_known_hosts"))


if __name__ == "__main__":
    unittest.main()
