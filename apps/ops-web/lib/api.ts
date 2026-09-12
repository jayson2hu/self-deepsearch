import type {
  AdminFeedbackListResponse,
  AuditLogListResponse,
  CatalogEntityListResponse,
  ConflictReviewListResponse,
  ContentRevisionListResponse,
  EditorialRecommendationListResponse,
  DiscoveryMixRule,
  InvitationListResponse,
  OperationsSystemHealth,
  OperationsUserListResponse,
  ReviewTaskListResponse,
  SessionResponse,
  TakedownListResponse,
  UserSummary,
  WorkImportBatchListResponse,
} from "@self-deepsearch/api-contracts";
import { cookies } from "next/headers";

const apiURL = process.env.PLATFORM_API_URL ?? "http://127.0.0.1:8080";

async function privateRequest(path: string): Promise<Response | null> {
  const token = (await cookies()).get("sd_session")?.value;
  if (!token) return null;
  try {
    return await fetch(`${apiURL}${path}`, {
      headers: { Accept: "application/json", Cookie: `sd_session=${token}` },
      cache: "no-store",
      signal: AbortSignal.timeout(3000),
    });
  } catch {
    return null;
  }
}

export async function getOperator(): Promise<UserSummary | null> {
  const response = await privateRequest("/api/v1/me");
  if (!response?.ok) return null;
  const user = ((await response.json()) as SessionResponse).user;
  return user.role === "user" ? null : user;
}

export async function getOperationsDashboard(includeSystemHealth: boolean): Promise<{
  tasks: ReviewTaskListResponse | null;
  approved: ReviewTaskListResponse | null;
  health: OperationsSystemHealth | null;
}> {
  const [taskResponse, approvedResponse, healthResponse] = await Promise.all([
    privateRequest("/admin/v1/review-tasks?status=open"),
    privateRequest("/admin/v1/review-tasks?status=approved"),
    includeSystemHealth ? privateRequest("/admin/v1/system/health") : Promise.resolve(null),
  ]);
  return {
    tasks: taskResponse?.ok ? ((await taskResponse.json()) as ReviewTaskListResponse) : null,
    approved: approvedResponse?.ok ? ((await approvedResponse.json()) as ReviewTaskListResponse) : null,
    health: healthResponse?.ok ? ((await healthResponse.json()) as OperationsSystemHealth) : null,
  };
}

export async function getOperationsUsers(): Promise<OperationsUserListResponse | null> {
  const response = await privateRequest("/admin/v1/users");
  return response?.ok ? ((await response.json()) as OperationsUserListResponse) : null;
}

export async function getAuditLogs(requestID: string): Promise<AuditLogListResponse | null> {
  const response = await privateRequest(`/admin/v1/audit-logs?request_id=${encodeURIComponent(requestID)}`);
  return response?.ok ? ((await response.json()) as AuditLogListResponse) : null;
}

export async function getReviewTasks(status = "open"): Promise<ReviewTaskListResponse | null> {
  const response = await privateRequest(`/admin/v1/review-tasks?status=${encodeURIComponent(status)}`);
  return response?.ok ? ((await response.json()) as ReviewTaskListResponse) : null;
}

export async function getOperationsInvitations(status = "all"): Promise<InvitationListResponse | null> {
  const response = await privateRequest(`/admin/v1/invitations?status=${encodeURIComponent(status)}`);
  return response?.ok ? ((await response.json()) as InvitationListResponse) : null;
}

export async function getTakedownRequests(): Promise<TakedownListResponse | null> {
  const response = await privateRequest("/admin/v1/takedowns?status=all");
  return response?.ok ? ((await response.json()) as TakedownListResponse) : null;
}

export async function getCatalogEntities(): Promise<CatalogEntityListResponse | null> {
  const response = await privateRequest("/admin/v1/entities?entity_type=all&status=all");
  return response?.ok ? ((await response.json()) as CatalogEntityListResponse) : null;
}

export async function getEditorialRecommendations(): Promise<EditorialRecommendationListResponse | null> {
  const response = await privateRequest("/admin/v1/editorial-recommendations?status=all");
  return response?.ok ? ((await response.json()) as EditorialRecommendationListResponse) : null;
}

export async function getDiscoveryMixRule(): Promise<DiscoveryMixRule | null> {
  const response = await privateRequest("/admin/v1/site-settings/discovery-mix");
  return response?.ok ? ((await response.json()) as DiscoveryMixRule) : null;
}

export async function getConflictReviews(): Promise<ConflictReviewListResponse | null> {
  const response = await privateRequest("/admin/v1/conflicts?status=all");
  return response?.ok ? ((await response.json()) as ConflictReviewListResponse) : null;
}

export async function getWorkImportBatches(): Promise<WorkImportBatchListResponse | null> {
  const response = await privateRequest("/admin/v1/imports/works");
  return response?.ok ? ((await response.json()) as WorkImportBatchListResponse) : null;
}

export async function getFeedbackReviewQueue(): Promise<AdminFeedbackListResponse | null> {
  const response = await privateRequest("/admin/v1/feedback?status=all");
  return response?.ok ? ((await response.json()) as AdminFeedbackListResponse) : null;
}

export async function getContentRevisions(entityType: string, entityID: string): Promise<ContentRevisionListResponse | null> {
  const response = await privateRequest(`/admin/v1/entities/${encodeURIComponent(entityID)}/revisions?entity_type=${encodeURIComponent(entityType)}`);
  return response?.ok ? ((await response.json()) as ContentRevisionListResponse) : null;
}
