from pydantic import BaseModel, Field
from typing import List, Optional, Dict, Any
import uuid

class ChapterData(BaseModel):
    type: str = Field(..., description="e.g., introduction, literature_review")
    title: str = Field(..., description="Title of the chapter")
    content: str = Field(..., description="Full content of the chapter")

class ReferenceData(BaseModel):
    citation_apa: Optional[str] = None # Assuming we primarily use APA for now
    # Add other fields if needed by docx (e.g., full reference details for different styles)

class DocumentGenerationRequest(BaseModel):
    project_id: uuid.UUID
    research_title: str
    student_name: Optional[str] = "A. Student" # Placeholder
    university_name: Optional[str] = "University of Example"
    specialization: Optional[str] = "Field of Study"
    chapters: List[ChapterData]
    references: Optional[List[ReferenceData]] = []
    formatting_options: Optional[Dict[str, Any]] = {} # e.g., {"citation_style": "APA", "font": "Times New Roman"}

class DocumentGenerationResponse(BaseModel):
    project_id: uuid.UUID
    file_name: str
    message: str
    # file_content_base64: Optional[str] = None # If returning file directly, not recommended for large files
    # Instead, the service will save the file and Go backend will fetch it or provide a download link.