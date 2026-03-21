"""
Stage 2 demo: end-to-end embeddings and semantic search using just Python.
Run: python/.venv/bin/python python/demo.py
"""

import os
import sys

import database
import embeddings

DB_PATH = os.path.join(
    os.path.expanduser("~"), ".local", "share", "semantic-files", "semantic_files.db"
)

SCHEMA = """
CREATE TABLE IF NOT EXISTS tracked_directories (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    path TEXT NOT NULL UNIQUE,
    added_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS indexed_files (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    directory_id INTEGER NOT NULL REFERENCES tracked_directories(id),
    path TEXT NOT NULL UNIQUE,
    file_hash TEXT NOT NULL,
    file_size INTEGER,
    modified_at TIMESTAMP,
    indexed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS embeddings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    file_id INTEGER NOT NULL REFERENCES indexed_files(id) ON DELETE CASCADE,
    chunk_index INTEGER NOT NULL,
    chunk_text TEXT NOT NULL,
    embedding BLOB NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
"""

SAMPLE_DOCS = {
    "cooking_notes.txt": (
        "Grandma's tomato sauce recipe requires fresh basil, garlic, and San Marzano tomatoes. "
        "Simmer on low heat for at least two hours, stirring occasionally. "
        "Add a pinch of sugar if the tomatoes are too acidic.\n\n"
        "For the pasta, always use a large pot with plenty of salted water. "
        "Cook until al dente, then toss directly into the sauce. "
        "Reserve some pasta water to adjust the consistency."
    ),
    "meeting_notes.txt": (
        "Q1 planning meeting - March 5th.\n\n"
        "We discussed the roadmap for the next quarter. "
        "The team agreed to prioritize the new search feature and defer the dashboard redesign. "
        "Budget was approved for two new hires in engineering.\n\n"
        "Action items: Sarah will draft the job descriptions by Friday. "
        "Mike will set up the new CI pipeline. "
        "Next sync is scheduled for March 19th."
    ),
    "travel_journal.txt": (
        "Day 3 in Kyoto. Visited Fushimi Inari shrine early in the morning to avoid crowds. "
        "The thousands of vermillion torii gates winding up the mountain were breathtaking. "
        "Had amazing street food near the station - takoyaki and matcha ice cream.\n\n"
        "In the afternoon, walked through the bamboo grove in Arashiyama. "
        "The sound of wind through the bamboo was incredibly peaceful. "
        "Ended the day with a traditional kaiseki dinner at a small ryokan."
    ),
    "python_notes.txt": (
        "Python virtual environments isolate project dependencies. "
        "Use 'python -m venv .venv' to create one, then activate it with 'source .venv/bin/activate'. "
        "Always pin your dependencies in requirements.txt for reproducibility.\n\n"
        "Type hints improve code readability and catch bugs early with mypy. "
        "Use 'from __future__ import annotations' for forward references. "
        "Dataclasses are great for simple value objects."
    ),
    "garden_log.txt": (
        "Planted tomato seedlings and basil in the raised bed today. "
        "The soil pH tested at 6.5 which is ideal for most vegetables. "
        "Set up drip irrigation on a timer for early morning watering.\n\n"
        "The sunflowers from last month are already 2 feet tall. "
        "Need to add stakes before they get top-heavy. "
        "Noticed some aphids on the rose bushes - will try neem oil spray tomorrow."
    ),
}


def chunk_text(text: str, max_chars: int = 500) -> list[str]:
    paragraphs = [p.strip() for p in text.split("\n\n") if p.strip()]
    chunks = []
    current = ""
    for para in paragraphs:
        if current and len(current) + len(para) + 1 > max_chars:
            chunks.append(current)
            current = para
        else:
            current = f"{current} {para}".strip() if current else para
    if current:
        chunks.append(current)
    return chunks


def index_samples(conn):
    existing = conn.execute("SELECT COUNT(*) FROM embeddings").fetchone()[0]
    if existing > 0:
        print(f"Database already has {existing} embeddings. Skipping indexing.")
        return

    # Create a fake tracked directory
    conn.execute(
        "INSERT OR IGNORE INTO tracked_directories (path) VALUES (?)", ("demo",)
    )
    conn.commit()
    dir_id = conn.execute(
        "SELECT id FROM tracked_directories WHERE path = ?", ("demo",)
    ).fetchone()[0]

    total_chunks = 0
    for filename, content in SAMPLE_DOCS.items():
        print(f"  Indexing {filename}...")
        # Create indexed file entry
        conn.execute(
            "INSERT OR IGNORE INTO indexed_files (directory_id, path, file_hash, file_size) VALUES (?, ?, ?, ?)",
            (dir_id, filename, "demo", len(content)),
        )
        conn.commit()
        file_id = conn.execute(
            "SELECT id FROM indexed_files WHERE path = ?", (filename,)
        ).fetchone()[0]

        chunks = chunk_text(content)
        for i, chunk in enumerate(chunks):
            vec = embeddings.generate_embedding(chunk)
            database.store_embedding(conn, file_id, i, chunk, vec)
            total_chunks += 1

    print(f"  Indexed {len(SAMPLE_DOCS)} documents ({total_chunks} chunks).\n")


def search(conn, query: str, top_k: int = 5):
    query_vec = embeddings.generate_embedding(query)
    all_embs = database.get_all_embeddings(conn)

    scored = []
    for emb_id, file_id, chunk_text, vec in all_embs:
        score = embeddings.compute_similarity(query_vec, vec)
        # Look up filename
        row = conn.execute(
            "SELECT path FROM indexed_files WHERE id = ?", (file_id,)
        ).fetchone()
        filename = row[0] if row else "unknown"
        scored.append((score, filename, chunk_text))

    scored.sort(key=lambda x: x[0], reverse=True)
    return scored[:top_k]


def main():
    os.makedirs(os.path.dirname(DB_PATH), exist_ok=True)
    conn = database.connect(DB_PATH)
    conn.executescript(SCHEMA)

    print("=== Semantic File Search Demo ===\n")
    print("Indexing sample documents...")
    index_samples(conn)

    print("Ready for search queries. Type 'quit' to exit.\n")
    while True:
        try:
            query = input("Search: ").strip()
        except (EOFError, KeyboardInterrupt):
            break
        if not query or query.lower() == "quit":
            break

        results = search(conn, query)
        print(f"\nTop {len(results)} results for '{query}':\n")
        for i, (score, filename, text) in enumerate(results, 1):
            preview = text[:120] + "..." if len(text) > 120 else text
            print(f"  {i}. [{score:.4f}] {filename}")
            print(f"     {preview}\n")

    conn.close()
    print("Done.")


if __name__ == "__main__":
    main()
