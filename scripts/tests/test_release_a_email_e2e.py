from __future__ import annotations

import importlib.util
import io
import os
import sys
import unittest
from contextlib import redirect_stderr
from pathlib import Path
from unittest import mock

SCRIPT = Path(__file__).resolve().parents[1] / "release_a_email_e2e.py"
SPEC = importlib.util.spec_from_file_location("release_a_email_e2e", SCRIPT)
assert SPEC and SPEC.loader
MODULE = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = MODULE
SPEC.loader.exec_module(MODULE)


class ReleaseAEmailE2ETests(unittest.TestCase):
    def test_extract_code_reads_nested_decoded_mail_body(self) -> None:
        payload = {"Text": "密码重置验证码：123456\r\n10 分钟内有效", "Attachments": []}
        self.assertEqual(MODULE.extract_code(payload), "123456")

    def test_extract_code_does_not_accept_unlabelled_numbers(self) -> None:
        self.assertIsNone(MODULE.extract_code({"Text": "created 20260820, id 123456"}))

    def test_mailpit_message_shapes_and_recipient_matching(self) -> None:
        message = {"ID": "message-1", "To": [{"Address": "release-a@example.test"}], "Subject": "幕鉴注册验证码"}
        self.assertEqual(MODULE.mailpit_messages({"messages": [message]}), [message])
        self.assertEqual(MODULE.message_id(message), "message-1")
        self.assertTrue(MODULE.contains_text(message, "RELEASE-A@example.test"))

    def test_invitation_passwords_are_read_from_named_environment_variables(self) -> None:
        with mock.patch.dict(os.environ, {"TEST_OWNER_PASSWORD": "secret-value"}, clear=False):
            self.assertEqual(MODULE.required_secret("TEST_OWNER_PASSWORD"), "secret-value")
        with mock.patch.dict(os.environ, {}, clear=True):
            with self.assertRaisesRegex(MODULE.E2EFailure, "TEST_OWNER_PASSWORD"):
                MODULE.required_secret("TEST_OWNER_PASSWORD")

    def test_owner_password_is_not_accepted_as_a_command_line_argument(self) -> None:
        with redirect_stderr(io.StringIO()), self.assertRaises(SystemExit):
            MODULE.parse_args(["--owner-password", "must-not-appear-in-process-list"])

    def test_invitation_confirms_password_before_requesting_mail_and_checks_role(self) -> None:
        for role in ("user", "editor", "admin"):
            with self.subTest(role=role):
                owner, invited = mock.Mock(), mock.Mock()
                owner.request.side_effect = [
                    MODULE.HTTPResult(200, {}), MODULE.HTTPResult(200, {}),
                    MODULE.HTTPResult(201, {"id": "invitation-id"}),
                ]
                owner.message_ids.return_value = {"previous-mail"}
                owner.wait_for_code.return_value = "123456"
                invited.request.side_effect = [
                    MODULE.HTTPResult(201, {}), MODULE.HTTPResult(200, {"user": {"role": role}}),
                ]
                with mock.patch.object(MODULE, "ReleaseAClient", side_effect=[owner, invited]):
                    MODULE.run_invitation(
                        mock.Mock(), owner_email="owner@example.test", owner_password="private-password",
                        invited_email="invited@example.test", invited_password="private-invited-password",
                        invited_role=role,
                    )
                self.assertEqual(
                    [call.args[1] for call in owner.request.call_args_list],
                    ["/api/v1/auth/login", "/api/v1/auth/reauth", "/admin/v1/invitations"],
                )
                self.assertEqual(owner.request.call_args.args[2]["role"], role)

    def test_rejected_password_confirmation_never_creates_invitation(self) -> None:
        owner = mock.Mock()
        owner.request.side_effect = [MODULE.HTTPResult(200, {}), MODULE.HTTPResult(401, {})]
        with mock.patch.object(MODULE, "ReleaseAClient", return_value=owner):
            with self.assertRaisesRegex(MODULE.E2EFailure, "owner recent password confirmation"):
                MODULE.run_invitation(
                    mock.Mock(), owner_email="owner@example.test", owner_password="private-password",
                    invited_email="invited@example.test", invited_password="private-invited-password",
                )
        self.assertEqual(owner.request.call_count, 2)
        owner.message_ids.assert_not_called()

    def test_invitation_rejects_owner_role_before_any_network_call(self) -> None:
        with mock.patch.object(MODULE, "ReleaseAClient") as client:
            with self.assertRaisesRegex(MODULE.E2EFailure, "unsupported invitation role"):
                MODULE.run_invitation(
                    mock.Mock(), owner_email="owner@example.test", owner_password="private-password",
                    invited_email="invited@example.test", invited_password="private-invited-password",
                    invited_role="owner",
                )
        client.assert_not_called()


if __name__ == "__main__":
    unittest.main()
