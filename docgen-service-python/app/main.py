from fastapi import FastAPI, HTTPException, BackgroundTasks, status
from fastapi.responses import FileResponse, JSONResponse
import logging
import os
import uuid # For filename generation if needed directly here

from .models import DocumentGenerationRequest, DocumentGenerationResponse
from .generator import create_research_document

# Configure logging
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

app = FastAPI(
    title="Research Document Generation Service",
    description="Generates DOCX research papers.",
    version="0.1.0"
)

# Ensure output directory exists
OUTPUT_DIR = os.getenv("DOCGEN_OUTPUT_DIR", "./generated_documents")
if not os.path.exists(OUTPUT_DIR):
    os.makedirs(OUTPUT_DIR)
    logger.info(f"Created output directory: {OUTPUT_DIR}")


@app.post("/generate-document", response_model=DocumentGenerationResponse, status_code=status.HTTP_202_ACCEPTED)
async def generate_document_endpoint(request_data: DocumentGenerationRequest, background_tasks: BackgroundTasks):
    """
    Accepts research data and initiates document generation.
    The actual generation happens in the background.
    The Go backend will later be notified or poll for completion.
    """
    logger.info(f"Received document generation request for project ID: {request_data.project_id}")

    # For now, synchronous generation for simplicity in MVP.
    # For true async, you'd queue this task (e.g., Celery, RabbitMQ)
    # and the Go backend would poll or get a webhook.
    try:
        # This call is blocking in this simple setup
        file_name, file_path = create_research_document(request_data, OUTPUT_DIR)
        
        # This is where you'd notify the Go backend of completion.
        # For MVP, Go backend can poll/check the `generated_documents` table status,
        # which it updated to 'processing' and this service effectively makes 'completed'.
        # The Go service will update the DB record to 'completed' and store file_path.

        logger.info(f"Document {file_name} generated successfully for project {request_data.project_id}. Stored at {file_path}")
        return DocumentGenerationResponse(
            project_id=request_data.project_id,
            file_name=file_name,
            message="Document generation initiated and completed. Go backend should update DB.",
        )
    except Exception as e:
        logger.error(f"Failed to generate document for project {request_data.project_id}: {e}", exc_info=True)
        # Notify Go backend of failure if possible, or it will timeout on polling.
        raise HTTPException(status_code=status.HTTP_500_INTERNAL_SERVER_ERROR, detail=f"Document generation failed: {str(e)}")


# This endpoint is more for direct testing of the Python service or if Go service pulls the file.
# In the planned architecture, Go service updates its DB with file_path and serves the download.
@app.get("/download/{file_name}")
async def download_generated_document(file_name: str):
    file_path = os.path.join(OUTPUT_DIR, file_name)
    if not os.path.exists(file_path):
        logger.warn(f"Download request for non-existent file: {file_name}")
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="File not found.")
    
    logger.info(f"Serving file for download: {file_name}")
    return FileResponse(path=file_path, filename=file_name, media_type='application/vnd.openxmlformats-officedocument.wordprocessingml.document')


@app.get("/health")
async def health_check():
    return {"status": "healthy"}

if __name__ == "__main__":
    import uvicorn
    # For local development, ensure you have a .env file in docgen-service-python/
    # or set DOCGEN_OUTPUT_DIR environment variable.
    # from dotenv import load_dotenv
    # load_dotenv() # To load .env variables if any specific to python service
    uvicorn.run(app, host="0.0.0.0", port=8001, log_level="info")