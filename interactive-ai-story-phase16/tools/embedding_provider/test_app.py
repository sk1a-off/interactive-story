import importlib.util
import sys
import types
import unittest
from pathlib import Path
from typing import get_args


class FakeFastAPI:
    def get(self, _path):
        return lambda function: function

    def post(self, _path):
        return lambda function: function


class FakeModel:
    def __init__(self, *_args, **_kwargs):
        self.calls = 0

    def encode(self, *_args, **_kwargs):
        self.calls += 1
        raise AssertionError("encode must not run for an empty batch")


class FieldDefinition:
    def __init__(self, **kwargs):
        self.kwargs = kwargs


class FixedModel:
    def __init__(self, vectors):
        self.vectors = vectors

    def encode(self, *_args, **_kwargs):
        return self.vectors


class FakeVectors:
    def __init__(self, shape, values):
        self.ndim = len(shape)
        self.shape = shape
        self.values = values

    def tolist(self):
        return self.values


def load_app_module():
    fastapi = types.ModuleType("fastapi")
    fastapi.FastAPI = FakeFastAPI
    pydantic = types.ModuleType("pydantic")
    pydantic.BaseModel = object
    pydantic.Field = lambda **kwargs: FieldDefinition(**kwargs)
    sentence_transformers = types.ModuleType("sentence_transformers")
    sentence_transformers.SentenceTransformer = FakeModel

    previous = {name: sys.modules.get(name) for name in ("fastapi", "pydantic", "sentence_transformers")}
    sys.modules.update(
        {
            "fastapi": fastapi,
            "pydantic": pydantic,
            "sentence_transformers": sentence_transformers,
        }
    )
    try:
        path = Path(__file__).with_name("app.py")
        spec = importlib.util.spec_from_file_location("embedding_provider_app_test", path)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        return module
    finally:
        for name, original in previous.items():
            if original is None:
                sys.modules.pop(name, None)
            else:
                sys.modules[name] = original


class EmbeddingProviderContractTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.module = load_app_module()

    def test_mode_is_restricted_to_supported_values(self):
        self.assertEqual(get_args(self.module.Request.__annotations__["mode"]), ("query", "document"))

    def test_batch_size_is_bounded(self):
        field = self.module.Request.__dict__["texts"]
        self.assertEqual(field.kwargs["max_length"], self.module.MAX_BATCH_SIZE)

    def test_empty_batch_returns_empty_vectors_without_loading_inference(self):
        response = self.module.embed(types.SimpleNamespace(mode="query", texts=[]))
        self.assertEqual(response, {"vectors": []})
        self.assertEqual(self.module.model.calls, 0)

    def test_non_empty_batch_requires_one_vector_per_text(self):
        original = self.module.model
        self.module.model = FixedModel(FakeVectors((1, self.module.EMBEDDING_DIMENSIONS), [[0.0] * 384]))
        try:
            with self.assertRaisesRegex(RuntimeError, "unexpected embedding shape"):
                self.module.embed(types.SimpleNamespace(mode="document", texts=["one", "two"]))
        finally:
            self.module.model = original

    def test_valid_batch_is_serialized(self):
        values = [[0.0] * self.module.EMBEDDING_DIMENSIONS]
        original = self.module.model
        self.module.model = FixedModel(FakeVectors((1, self.module.EMBEDDING_DIMENSIONS), values))
        try:
            self.assertEqual(
                self.module.embed(types.SimpleNamespace(mode="query", texts=["one"])),
                {"vectors": values},
            )
        finally:
            self.module.model = original


if __name__ == "__main__":
    unittest.main()
