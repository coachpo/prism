from __future__ import annotations

import sys
import unittest
from pathlib import Path

SCRIPTS = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(SCRIPTS))

from ssh_command import ssh_command


class SSHCommandTests(unittest.TestCase):
    def test_remote_arguments_are_one_shell_quoted_command(self) -> None:
        command = ssh_command(
            "capy",
            [
                "docker",
                "buildx",
                "imagetools",
                "inspect",
                "ghcr.io/example/prism:v1.1.3",
                "--format",
                "{{json .Manifest}}",
            ],
        )
        self.assertEqual(command[:4], ["ssh", "-o", "BatchMode=yes", "capy"])
        self.assertEqual(len(command), 5)
        self.assertIn("--format '{{json .Manifest}}'", command[4])


if __name__ == "__main__":
    unittest.main()
