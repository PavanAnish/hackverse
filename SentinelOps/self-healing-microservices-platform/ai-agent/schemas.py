from enum import Enum
from pydantic import BaseModel, Field

class ActionEnum(str, Enum):
    restart_container = "restart_container"
    scale_replicas = "scale_replicas"
    clear_cache = "clear_cache"

class ServiceEnum(str, Enum):
    order_service = "order-service"
    payment_service = "payment-service"
    inventory_service = "inventory-service"

class DiagnosisResult(BaseModel):
    root_cause: str = Field(description="The diagnosed root cause of the incident")
    confidence: float = Field(description="Confidence level of the diagnosis (0.0 to 1.0)")
    recommended_action: ActionEnum = Field(description="The pre-validated action to take")
    target_service: ServiceEnum = Field(description="The service to apply the action to")
    expected_effect: str = Field(description="One sentence describing the expected effect of the action")
    risk_note: str = Field(description="One sentence describing the potential risk or downside")
    reversible: bool = Field(description="Whether the action can be easily reversed")
