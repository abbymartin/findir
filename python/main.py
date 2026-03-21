import json
import sys

def handle_request(req: dict, db_conn) -> dict:
    import database
    import embeddings
    action = req.get("action")

    if action == "embed":
        text = req["text"]
        vec = embeddings.generate_embedding(text)
        return {"embedding": vec}

    elif action == "search":
        query = req["query"]
        top_k = req.get("top_k", 5)
        query_vec = embeddings.generate_embedding(query)
        all_embeddings = database.get_all_embeddings(db_conn)

        scored = []
        for emb_id, file_id, chunk_text, vec in all_embeddings:
            score = embeddings.compute_similarity(query_vec, vec)
            row = db_conn.execute(
                "SELECT path FROM indexed_files WHERE id = ?", (file_id,)
            ).fetchone()
            file_path = row[0] if row else "unknown"
            scored.append({"file_path": file_path, "chunk_text": chunk_text, "score": score})

        scored.sort(key=lambda x: x["score"], reverse=True)
        return {"results": scored[:top_k]}

    elif action == "index_file":
        file_id = req["file_id"]
        chunks = req["chunks"]
        for i, chunk_text in enumerate(chunks):
            vec = embeddings.generate_embedding(chunk_text)
            database.store_embedding(db_conn, file_id, i, chunk_text, vec)
        return {"status": "ok", "chunks_indexed": len(chunks)}

    elif action == "warmup":
        embeddings.get_model()
        return {"status": "ok"}

    elif action == "ping":
        return {"status": "ok"}

    else:
        return {"error": f"unknown action: {action}"}


def main():
    import database
    db_path = sys.argv[1] if len(sys.argv) > 1 else "data/semantic_files.db"
    db_conn = database.connect(db_path)

    # Signal ready
    sys.stdout.write(json.dumps({"status": "ready"}) + "\n")
    sys.stdout.flush()

    # Preload embedding model in background
    import threading
    threading.Thread(target=lambda: __import__('embeddings').get_model(), daemon=True).start()

    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        try:
            req = json.loads(line)
            resp = handle_request(req, db_conn)
        except Exception as e:
            resp = {"error": str(e)}

        sys.stdout.write(json.dumps(resp) + "\n")
        sys.stdout.flush()

    db_conn.close()


if __name__ == "__main__":
    main()
