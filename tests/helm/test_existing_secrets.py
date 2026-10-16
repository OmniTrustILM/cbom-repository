import base64
import copy
from pathlib import Path
import subprocess
import unittest

import yaml


ROOT = Path(__file__).resolve().parents[2]
CHART = ROOT / "deploy/charts/cbom-repository"
CBOM_SECRET = "external-cbom-credentials"
MINIO_SECRET = "external-minio-environment"


def render(values):
    result = subprocess.run(
        ["helm", "template", "cbom-test", str(CHART), "--namespace", "cbom-test", "-f", "-"],
        input=yaml.safe_dump(values), text=True, capture_output=True, check=True,
    )
    documents = [doc for doc in yaml.safe_load_all(result.stdout) if doc]
    objects = {(doc["kind"], doc["metadata"]["name"]): doc for doc in documents}
    if len(objects) != len(documents):
        raise AssertionError("duplicate rendered resource identity")
    return objects


def credentials():
    return {
        "cbomRepo": {"accessKey": "synthetic-cbom-user", "secretKey": "synthetic-cbom-password"},
        "minio": {"enabled": True, "rootUser": "synthetic-minio-user",
                  "rootPassword": "synthetic-minio-password"},
    }


def cbom_containers(objects):
    spec = objects[("Deployment", "cbom-repository-deployment")]["spec"]["template"]["spec"]
    return [spec["containers"][0]] + spec.get("initContainers", [])


class ExistingSecretsTest(unittest.TestCase):
    def assert_cbom_references(self, objects, secret):
        for container in cbom_containers(objects):
            env = {item["name"]: item for item in container["env"]}
            for name, key in (("APP_S3_ACCESS_KEY", "access_key"), ("APP_S3_SECRET_KEY", "secret_key")):
                self.assertEqual(env[name], {"name": name, "valueFrom": {
                    "secretKeyRef": {"name": secret, "key": key}}})

    def assert_minio_reference(self, objects, secret):
        container = objects[("StatefulSet", "minio-statefulset")]["spec"]["template"]["spec"]["containers"][0]
        self.assertEqual(container["envFrom"][0], {"secretRef": {"name": secret}})

    def assert_absent_payload(self, objects, literals):
        serialized = yaml.safe_dump(list(objects.values()))
        for literal in literals:
            self.assertNotIn(literal, serialized)
            self.assertNotIn(base64.b64encode(literal.encode()).decode(), serialized)

    def test_legacy_credentials_and_browser_settings(self):
        for console in (False, True):
            with self.subTest(console=console):
                values = credentials()
                values["minio"]["console"] = {"enabled": console, "redirectUrl": "https://console.example.invalid"}
                objects = render(values)
                cbom = objects[("Secret", "cbom-repository-secret")]["data"]
                self.assertEqual({k: base64.b64decode(v).decode() for k, v in cbom.items()}, {
                    "access_key": "synthetic-cbom-user", "secret_key": "synthetic-cbom-password"})
                minio = objects[("Secret", "minio-secret")]["data"]
                expected = {"MINIO_ROOT_USER": "synthetic-minio-user", "MINIO_ROOT_PASSWORD": "synthetic-minio-password",
                            "MINIO_BROWSER": "on" if console else "off"}
                if console:
                    expected["MINIO_BROWSER_REDIRECT_URL"] = "https://console.example.invalid"
                self.assertEqual({k: base64.b64decode(v).decode() for k, v in minio.items()}, expected)
                self.assert_cbom_references(objects, "cbom-repository-secret")
                self.assert_minio_reference(objects, "minio-secret")

    def test_external_modes_suppress_credentials_independently(self):
        for cbom_external, minio_external in ((True, False), (False, True), (True, True)):
            with self.subTest(cbom=cbom_external, minio=minio_external):
                values = credentials()
                if cbom_external:
                    values["cbomRepo"]["existingSecret"] = CBOM_SECRET
                if minio_external:
                    values["minio"]["existingSecret"] = MINIO_SECRET
                objects = render(values)
                self.assertEqual(("Secret", "cbom-repository-secret") in objects, not cbom_external)
                self.assertEqual(("Secret", "minio-secret") in objects, not minio_external)
                self.assert_cbom_references(objects, CBOM_SECRET if cbom_external else "cbom-repository-secret")
                self.assert_minio_reference(objects, MINIO_SECRET if minio_external else "minio-secret")
                if cbom_external:
                    self.assert_absent_payload(objects, list(values["cbomRepo"][k] for k in ("accessKey", "secretKey")))
                if minio_external:
                    self.assert_absent_payload(objects, list(values["minio"][k] for k in ("rootUser", "rootPassword")))

    def test_external_mode_accepts_omitted_and_null_literals(self):
        for literal in ("omitted", None):
            with self.subTest(literal=literal):
                values = {"cbomRepo": {"existingSecret": CBOM_SECRET},
                          "minio": {"enabled": True, "existingSecret": MINIO_SECRET}}
                if literal is None:
                    values["cbomRepo"].update(accessKey=None, secretKey=None)
                    values["minio"].update(rootUser=None, rootPassword=None)
                objects = render(values)
                self.assertFalse(any(kind == "Secret" for kind, name in objects))
                self.assert_cbom_references(objects, CBOM_SECRET)
                self.assert_minio_reference(objects, MINIO_SECRET)

    def test_external_references_preserve_other_rendered_behavior(self):
        for persistence in ({"enabled": False}, {"enabled": True},
                            {"enabled": True, "existingClaim": "preserved-data"}):
            for console in (False, True):
                with self.subTest(persistence=persistence, console=console):
                    values = credentials()
                    values["minio"].update(persistence=persistence, console={"enabled": console})
                    values["additionalEnv"] = {"variables": [{"name": "CUSTOM_ENV", "value": "synthetic"}]}
                    values["image"] = {"digest": "sha256:" + "a" * 64}
                    before = render(values)
                    external = copy.deepcopy(values)
                    external["cbomRepo"]["existingSecret"] = CBOM_SECRET
                    external["minio"]["existingSecret"] = MINIO_SECRET
                    after = render(external)
                    del before[("Secret", "cbom-repository-secret")]
                    del before[("Secret", "minio-secret")]
                    for container in cbom_containers(after):
                        for env in container["env"]:
                            if env["name"] in ("APP_S3_ACCESS_KEY", "APP_S3_SECRET_KEY"):
                                env["valueFrom"]["secretKeyRef"]["name"] = "cbom-repository-secret"
                    container = after[("StatefulSet", "minio-statefulset")]["spec"]["template"]["spec"]["containers"][0]
                    container["envFrom"][0]["secretRef"]["name"] = "minio-secret"
                    self.assertEqual(before, after)

    def test_disabled_minio_remains_absent_with_external_reference(self):
        objects = render({"cbomRepo": {"existingSecret": CBOM_SECRET},
                          "minio": {"enabled": False, "existingSecret": MINIO_SECRET}})
        self.assertNotIn(("StatefulSet", "minio-statefulset"), objects)
        self.assertNotIn(("Service", "minio-service"), objects)
        self.assertNotIn(("Secret", "minio-secret"), objects)
        self.assertEqual(len(cbom_containers(objects)), 1)
        self.assert_cbom_references(objects, CBOM_SECRET)

    def test_external_names_cannot_delete_previous_chart_secrets(self):
        for component, name in (("cbomRepo", "cbom-repository-secret"), ("minio", "minio-secret")):
            with self.subTest(component=component):
                values = credentials()
                values[component]["existingSecret"] = name
                with self.assertRaises(subprocess.CalledProcessError) as error:
                    render(values)
                self.assertIn(component + ".existingSecret must differ from " + name, error.exception.stderr)

    def test_empty_names_preserve_legacy_render(self):
        values = credentials()
        before = render(values)
        values["cbomRepo"]["existingSecret"] = ""
        values["minio"]["existingSecret"] = ""
        self.assertEqual(before, render(values))


if __name__ == "__main__":
    unittest.main()
