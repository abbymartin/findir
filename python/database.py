import sqlite3
import struct
from pathlib import Path


def connect(db_path: str) -> sqlite3.Connection:
    conn = sqlite3.connect(db_path)
    conn.execute("PRAGMA foreign_keys = ON")
    return conn


def store_embedding(
    conn: sqlite3.Connection,
    file_id: int,
    chunk_index: int,
    chunk_text: str,
    embedding: list[float],
) -> int:
    blob = struct.pack(f"{len(embedding)}f", *embedding)
    cursor = conn.execute(
        "INSERT INTO embeddings (file_id, chunk_index, chunk_text, embedding) VALUES (?, ?, ?, ?)",
        (file_id, chunk_index, chunk_text, blob),
    )
    conn.commit()
    return cursor.lastrowid


def get_all_embeddings(conn: sqlite3.Connection) -> list[tuple[int, int, str, list[float]]]:
    rows = conn.execute(
        "SELECT id, file_id, chunk_text, embedding FROM embeddings"
    ).fetchall()
    results = []
    for row_id, file_id, chunk_text, blob in rows:
        dim = len(blob) // 4
        embedding = list(struct.unpack(f"{dim}f", blob))
        results.append((row_id, file_id, chunk_text, embedding))
    return results


def mark_file_indexed(
    conn: sqlite3.Connection,
    directory_id: int,
    path: str,
    file_hash: str,
    file_size: int,
    modified_at: str,
) -> int:
    cursor = conn.execute(
        "INSERT OR REPLACE INTO indexed_files (directory_id, path, file_hash, file_size, modified_at) VALUES (?, ?, ?, ?, ?)",
        (directory_id, path, file_hash, file_size, modified_at),
    )
    conn.commit()
    return cursor.lastrowid
