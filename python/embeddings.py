import numpy as np

MODEL_NAME = "sentence-transformers/all-MiniLM-L6-v2"

_model = None


def get_model():
    global _model
    if _model is None:
        import sys
        from fastembed import TextEmbedding
        _model = TextEmbedding(model_name=MODEL_NAME)
    return _model


def generate_embedding(text: str) -> list[float]:
    model = get_model()
    embedding = list(model.embed([text]))[0]
    return embedding.tolist()


def compute_similarity(vec1: list[float], vec2: list[float]) -> float:
    a = np.array(vec1)
    b = np.array(vec2)
    return float(np.dot(a, b))
