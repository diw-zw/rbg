# Dockerfile for NVIDIA AI Configurator
# This container is optimized for local model processing only.
# aiconfigurator is a pure-Python package, so no build tools are needed.
FROM docker.m.daocloud.io/python:3.12-slim-bookworm

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PYTHONFAULTHANDLER=1 \
    PIP_NO_CACHE_DIR=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1

# Install AI Configurator and dependencies.
# pyyaml is explicitly listed to ensure generate-rbg-yaml script can import it,
# even if aiconfigurator's dependency on it changes in future versions.
RUN pip install --no-cache-dir aiconfigurator==0.7.0 pyyaml

# Copy generate-rbg-yaml script
COPY generate-rbg-yaml.py /usr/local/bin/generate-rbg-yaml.py
RUN chmod +x /usr/local/bin/generate-rbg-yaml.py

# Create workspace directory
WORKDIR /workspace

# Create a non-root user for security
RUN useradd -m -u 1000 aiconfigurator && \
    chown -R aiconfigurator:aiconfigurator /workspace
USER aiconfigurator

# Verify installation
RUN aiconfigurator version

# Set entrypoint to generate-rbg-yaml which orchestrates the full pipeline
ENTRYPOINT ["generate-rbg-yaml.py"]
CMD ["--help"]
