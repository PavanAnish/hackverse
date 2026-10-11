import os
import glob
import json
import asyncio
import asyncpg
from schemas import DiagnosisResult
from llm_wrapper import call_llm
from pydantic import ValidationError
import numpy as np

DB_URL = os.getenv("DATABASE_URL")

def get_embedding(text: str) -> list:
    """
    Very crude deterministic embedding for demonstration without heavy ML libraries.
    In production, use OpenAI/Gemini embeddings.
    """
    # Simple 3-dimensional embedding based on character counts to differentiate them
    v1 = text.lower().count("payment") + text.lower().count("stuck")
    v2 = text.lower().count("order") + text.lower().count("cache")
    v3 = text.lower().count("inventory") + text.lower().count("scale")
    # Add a small epsilon to avoid zero vector
    vec = np.array([v1 + 0.1, v2 + 0.1, v3 + 0.1])
    vec = vec / np.linalg.norm(vec)
    return vec.tolist()

async def setup_db():
    conn = await asyncpg.connect(DB_URL)
    await conn.execute('CREATE EXTENSION IF NOT EXISTS vector')
    await conn.execute('''
        CREATE TABLE IF NOT EXISTS runbooks (
            id SERIAL PRIMARY KEY,
            content TEXT,
            embedding vector(3)
        )
    ''')
    await conn.execute('TRUNCATE TABLE runbooks')
    
    # Load runbooks
    for file_path in glob.glob('runbooks/*.md'):
        with open(file_path, 'r') as f:
            content = f.read()
            emb = get_embedding(content)
            await conn.execute('INSERT INTO runbooks (content, embedding) VALUES ($1, $2)', content, str(emb))
            
    await conn.close()

async def diagnose(incident_msg: str) -> tuple[DiagnosisResult, str]:
    conn = await asyncpg.connect(DB_URL)
    emb = get_embedding(incident_msg)
    
    # Cosine distance (<=>) search
    rows = await conn.fetch('''
        SELECT content FROM runbooks 
        ORDER BY embedding <=> $1 
        LIMIT 1
    ''', str(emb))
    
    context = rows[0]['content'] if rows else ""
    await conn.close()

    prompt = f"Incident: {incident_msg}\n\nRelevant Runbook:\n{context}\n\nDiagnose the issue."
    
    for attempt in range(2):
        try:
            raw_response = call_llm(prompt)
            # Find json block if any
            if "{" in raw_response:
                raw_response = raw_response[raw_response.find("{"):raw_response.rfind("}")+1]
            data = json.loads(raw_response)
            result = DiagnosisResult(**data)
            return result, context
        except (json.JSONDecodeError, ValidationError) as e:
            if attempt == 1:
                print(f"Validation failed twice: {e}")
                # Escalate to human
                raise Exception("Escalated to human: LLM failed to produce valid structure")
            print(f"Retrying LLM due to validation error: {e}")
