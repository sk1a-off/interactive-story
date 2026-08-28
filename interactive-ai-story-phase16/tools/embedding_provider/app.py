from fastapi import FastAPI
from pydantic import BaseModel
from sentence_transformers import SentenceTransformer

MODEL_NAME = "deepvk/USER2-small"
model = SentenceTransformer(MODEL_NAME, device="cpu")
app = FastAPI()

class Request(BaseModel):
    mode: str
    texts: list[str]

@app.get("/health")
def health():
    return {"status": "ready", "model": MODEL_NAME, "dimensions": 384, "device": "cpu"}

@app.post("/embed")
def embed(req: Request):
    if req.mode not in {"query", "document"}:
        raise ValueError("mode must be query or document")
    vectors = model.encode(req.texts, normalize_embeddings=True, convert_to_numpy=True)
    if vectors.shape[1] != 384:
        raise RuntimeError(f"unexpected embedding dimension: {vectors.shape[1]}")
    return {"vectors": vectors.tolist()}
