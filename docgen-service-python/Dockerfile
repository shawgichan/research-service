# Use an official Python runtime as a parent image
FROM python:3.9-slim

# Set the working directory in the container
WORKDIR /app

# Copy the requirements file into the container at /app
COPY ./requirements.txt /app/

# Install any needed packages specified in requirements.txt
RUN pip install --no-cache-dir -r requirements.txt

# Copy the current directory contents into the container at /app
COPY ./app /app/app

# Make port 8001 available to the world outside this container
EXPOSE 8001

# Define environment variable for output (can be overridden)
ENV DOCGEN_OUTPUT_DIR=/generated_documents_volume

# Create the output directory (this will be inside the container)
# For persistence, this directory should be mapped to a Docker volume.
RUN mkdir -p ${DOCGEN_OUTPUT_DIR}

# Run app.main:app when the container launches
# Use Gunicorn for production, Uvicorn for development is fine too
# CMD ["gunicorn", "-k", "uvicorn.workers.UvicornWorker", "-w", "4", "-b", "0.0.0.0:8001", "app.main:app"]
CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8001"]