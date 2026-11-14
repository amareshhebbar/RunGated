FROM python:3.12-slim
WORKDIR /app
COPY sdk/python /app/sdk/python
RUN pip install --no-cache-dir /app/sdk/python httpx openai langgraph langchain-core
COPY demo /app/demo
WORKDIR /app/demo
ENTRYPOINT ["python", "run_attacks.py"]
