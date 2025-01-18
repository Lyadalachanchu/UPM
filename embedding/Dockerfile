# Use a Python base image
FROM python:3.9-slim

# Set environment variables to prevent Python from buffering logs
ENV PYTHONDONTWRITEBYTECODE 1
ENV PYTHONUNBUFFERED 1

# Install dependencies
RUN apt-get update && apt-get install -y git && rm -rf /var/lib/apt/lists/*
RUN pip install --upgrade pip
COPY requirements.txt /app/requirements.txt
RUN pip install -r /app/requirements.txt

# Copy application files
COPY embedding_service.py /app/embedding_service.py

# Set the working directory
WORKDIR /app

# Expose the Flask app's port
EXPOSE 5000

# Command to run the app
CMD ["python", "embedding_service.py"]
