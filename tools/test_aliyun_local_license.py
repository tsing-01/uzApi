"""Exercise secret initialization with the real Compose dotenv parser, without a daemon."""

import base64
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
DOCKER = shutil.which("docker")
SEED = base64.b64encode(bytes(range(32))).decode()


class LicenseDeploymentTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.deploy = self.root / "deploy" / "aliyun"
        self.deploy.mkdir(parents=True)
        self.scripts = self.root / "scripts"
        self.scripts.mkdir()
        for name in ("aliyun-local-license.sh", "aliyun-deploy.sh"):
            shutil.copy(ROOT / "scripts" / name, self.scripts / name)
        self.compose = self.deploy / "docker-compose.yml"
        shutil.copy(ROOT / "deploy/aliyun/docker-compose.yml", self.compose)
        self.env_file = self.deploy / ".env.production"
        self.template = (ROOT / "deploy/aliyun/.env.production.example").read_text()
        self.env_file.write_text(self.template)
        self.env = {
            key: value for key, value in os.environ.items()
            if not key.startswith(("LOCAL_MODEL_ACCESS_", "COMPOSE_", "APP_IMAGE"))
            and key not in ("DOMAIN", "ENABLE_LOCAL_MODEL_ACCESS")
        }

    def configure(self, **values):
        with self.env_file.open("a") as file:
            for key, value in values.items():
                file.write(f"\n{key}={value}\n")

    def initialize(self, success=True):
        result = subprocess.run(
            ["bash", str(self.scripts / "aliyun-local-license.sh"),
             str(self.env_file), str(self.compose)],
            env=self.env, capture_output=True, text=True, check=False,
        )
        # Do not echo command output/environment on failures: they can contain secrets.
        self.assertEqual(result.returncode == 0, success)
        self.assertNotIn(SEED, result.stdout + result.stderr)
        return result

    def values(self):
        result = subprocess.run(
            [DOCKER, "compose", "--env-file", str(self.env_file), "-f",
             str(self.compose), "config", "--environment"],
            env=self.env, capture_output=True, text=True, check=True,
        )
        return dict(line.split("=", 1) for line in result.stdout.splitlines() if "=" in line)

    def test_initialize_and_redeploy_preserve_key_and_private_backup(self):
        result = self.initialize()
        values = self.values()
        seed = values["LOCAL_MODEL_ACCESS_SIGNING_SEED"]
        self.assertEqual(len(base64.b64decode(seed, validate=True)), 32)
        self.assertEqual(values["LOCAL_MODEL_ACCESS_ISSUER"], "https://api.uzapi.org")
        self.assertNotIn(seed, result.stdout + result.stderr)
        backups = list(self.deploy.glob(".env.production.before-license.*"))
        self.assertEqual(len(backups), 1)
        self.assertEqual(backups[0].read_text(), self.template)
        for file in (self.env_file, backups[0]):
            self.assertEqual(file.stat().st_mode & 0o777, 0o600)
        original = self.env_file.read_bytes()
        self.initialize()
        self.assertEqual(self.env_file.read_bytes(), original)
        self.assertEqual(len(list(self.deploy.glob(".env.production.before-license.*"))), 1)

    def test_quoted_existing_key_and_issuer_unchanged(self):
        self.configure(LOCAL_MODEL_ACCESS_SIGNING_SEED=f"'{SEED}'",
                       LOCAL_MODEL_ACCESS_ISSUER='"https://api.uzapi.org/licenses"')
        original = self.env_file.read_bytes()
        self.initialize()
        self.assertEqual(self.env_file.read_bytes(), original)

    def test_missing_issuer_keeps_existing_key(self):
        self.configure(LOCAL_MODEL_ACCESS_SIGNING_SEED=SEED)
        self.initialize()
        self.assertEqual(self.values()["LOCAL_MODEL_ACCESS_SIGNING_SEED"], SEED)

    def test_missing_key_keeps_existing_issuer(self):
        self.configure(LOCAL_MODEL_ACCESS_ISSUER="https://license.uzapi.org/v1")
        self.initialize()
        self.assertEqual(self.values()["LOCAL_MODEL_ACCESS_ISSUER"], "https://license.uzapi.org/v1")

    def test_explicit_default_issuer_with_noncanonical_caddy_address(self):
        for domain in (":80", '"uzapi.org, api.uzapi.org"'):
            with self.subTest(domain=domain):
                self.env_file.write_text(self.template)
                self.configure(DOMAIN=domain)
                self.env["LOCAL_MODEL_ACCESS_DEFAULT_ISSUER"] = "https://api.uzapi.org"
                self.initialize()
                self.assertEqual(self.values()["LOCAL_MODEL_ACCESS_ISSUER"], "https://api.uzapi.org")

    def test_default_issuer_does_not_override_persisted_issuer(self):
        self.env["LOCAL_MODEL_ACCESS_DEFAULT_ISSUER"] = "https://api.uzapi.org"
        self.configure(LOCAL_MODEL_ACCESS_ISSUER="https://license.uzapi.org/v1")
        self.initialize()
        self.assertEqual(self.values()["LOCAL_MODEL_ACCESS_ISSUER"], "https://license.uzapi.org/v1")

    def test_invalid_default_issuer_is_rejected(self):
        self.env["LOCAL_MODEL_ACCESS_DEFAULT_ISSUER"] = "http://api.uzapi.org"
        self.initialize(success=False)
        self.assertEqual(self.env_file.read_text(), self.template)

    def test_invalid_configuration_never_overwrites_environment(self):
        for values in (
            {"LOCAL_MODEL_ACCESS_SIGNING_SEED": "not-a-valid-key"},
            {"LOCAL_MODEL_ACCESS_ISSUER": "http://api.uzapi.org"},
            {"LOCAL_MODEL_ACCESS_ISSUER": "https://user@api.uzapi.org"},
            {"DOMAIN": ":80"},
        ):
            with self.subTest(keys=list(values)):
                self.env_file.write_text(self.template)
                self.configure(**values)
                original = self.env_file.read_bytes()
                self.initialize(success=False)
                self.assertEqual(self.env_file.read_bytes(), original)
                self.assertFalse(list(self.deploy.glob(".env.production.before-license.*")))

    def test_concurrent_initialization_is_rejected(self):
        Path(str(self.env_file) + ".license-lock").mkdir()
        self.initialize(success=False)
        self.assertEqual(self.env_file.read_text(), self.template)

    def test_healthy_app_with_disabled_license_fails_deployment(self):
        # Mock only daemon operations; config still uses the real Compose parser.
        binary_dir = self.root / "bin"
        binary_dir.mkdir()
        docker = binary_dir / "docker"
        docker.write_text('''#!/usr/bin/env bash
case " $* " in
  *" config "*|*" compose version "*) exec "$REAL_DOCKER" "$@" ;;
  *" exec "*"/local-model-access/keys"*) exit "$KEYS_STATUS" ;;
  *) exit 0 ;;
esac
''')
        docker.chmod(0o700)
        env = dict(self.env, ENABLE_LOCAL_MODEL_ACCESS="true", REAL_DOCKER=DOCKER,
                   PATH=str(binary_dir) + os.pathsep + self.env["PATH"])
        for status in (1, 0):
            with self.subTest(keys_status=status):
                result = subprocess.run(
                    ["bash", str(self.scripts / "aliyun-deploy.sh")],
                    env=dict(env, KEYS_STATUS=str(status)),
                    capture_output=True, text=True, check=False,
                )
                self.assertEqual(result.returncode, status)
                self.assertEqual("Local-model license signer is ready." in result.stdout, status == 0)


if __name__ == "__main__":
    unittest.main()
