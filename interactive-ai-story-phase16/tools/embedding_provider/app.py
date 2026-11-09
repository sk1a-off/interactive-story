from typing import Literal

from fastapi import FastAPI
from pydantic import BaseModel, Field
from sentence_transformers import SentenceTransformer

MODEL_NAME = "deepvk/USER2-small"
EMBEDDING_DIMENSIONS = 384
MAX_BATCH_SIZE = 64
model = SentenceTransformer(MODEL_NAME, device="cpu")
app = FastAPI()


class Request(BaseModel):
    mode: Literal["query", "document"]
    texts: list[str] = Field(max_length=MAX_BATCH_SIZE)


@app.get("/health")
def health():
    return {
        "status": "ready",
        "model": MODEL_NAME,
        "dimensions": EMBEDDING_DIMENSIONS,
        "device": "cpu",
        "maxBatchSize": MAX_BATCH_SIZE,
    }


@app.post("/embed")
def embed(req: Request):
    if not req.texts:
        return {"vectors": []}
    vectors = model.encode(req.texts, normalize_embeddings=True, convert_to_numpy=True)
    expected_shape = (len(req.texts), EMBEDDING_DIMENSIONS)
    if vectors.ndim != 2 or vectors.shape != expected_shape:
        raise RuntimeError(f"unexpected embedding shape: {vectors.shape}")
    return {"vectors": vectors.tolist()}
