export interface GateModeOption {
  id: "nexus" | "planning" | "verification" | "standard"
  label: string
  description: string
  current: boolean
  default?: boolean
}

/** The complete mode set stays visible even though the two switches remain the controls. */
export function gateModeOptions(planning: boolean, verification: boolean): GateModeOption[] {
  const current = planning && verification ? "nexus" : planning ? "planning" : verification ? "verification" : "standard"
  return [
    { id: "nexus", label: "NEXUS", description: "規劃檢查＋獨立驗證", current: current === "nexus" },
    { id: "planning", label: "規劃", description: "只要求規劃檢查（預設）", current: current === "planning", default: true },
    { id: "verification", label: "獨立驗證", description: "只要求 maker／checker 驗證", current: current === "verification" },
    { id: "standard", label: "一般流程", description: "不強制規劃或獨立驗證", current: current === "standard" },
  ]
}

/** One sentence for every settings combination; a cycle keeps the pair it captured on assignment. */
export function gateModeText(planning: boolean, verification: boolean): string {
  if (planning && verification) return "NEXUS：指派時擷取規劃與獨立驗證。Feature 和 Epic 先完成計劃檢查，完成程式後須由獨立 checker 通過才可 Merge；Issue 不需計劃檢查。"
  if (planning) return "指派時只擷取規劃：Feature 和 Epic 先完成計劃檢查；Merge 沿用一般驗證紀錄。Issue 不需計劃檢查。"
  if (verification) return "指派時只擷取獨立驗證：Epic 也略過強制計劃檢查；完成程式後仍須由獨立 checker 通過才可 Merge。"
  return "兩道 gate 都關閉：Epic 也略過強制計劃檢查；工作沿用一般執行與驗證流程。"
}
