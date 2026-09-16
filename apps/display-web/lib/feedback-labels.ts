import type { FeedbackItem } from "@self-deepsearch/api-contracts";

export const feedbackTypeLabels: Record<FeedbackItem["feedback_type"], string> = {
  correction: "资料纠错",
  source_suggestion: "来源建议",
  rights: "权利问题",
  other: "其他",
};

export const feedbackStatusLabels: Record<FeedbackItem["review_status"], string> = {
  pending: "已收到",
  reviewing: "处理中",
  accepted: "已采纳",
  rejected: "未采纳",
  closed: "已结束",
};
