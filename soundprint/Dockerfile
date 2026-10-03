# Marquee Soundprint analysis sidecar (D56). CUDA 12.8 PyTorch supports the RTX 50 series (sm_120).
FROM pytorch/pytorch:2.8.0-cuda12.8-cudnn9-runtime
RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY app.py .
ENV HF_HOME=/models HF_HUB_DISABLE_TELEMETRY=1 PYTHONUNBUFFERED=1 NUMBA_CACHE_DIR=/tmp/numba MPLCONFIGDIR=/tmp/mpl
EXPOSE 32501
CMD ["uvicorn", "app:app", "--host", "127.0.0.1", "--port", "32501", "--workers", "1"]
