/** One sentence for every settings combination; a cycle keeps the pair it captured on assignment. */
export function gateModeText(planning: boolean, verification: boolean): string {
  if (planning && verification) return "指派時擷取規劃與獨立驗證：Feature 和 Epic 先完成計劃檢查，完成程式後須由獨立 checker 通過才可 Merge。Issue 不需計劃檢查。"
  if (planning) return "指派時只擷取規劃：Feature 和 Epic 先完成計劃檢查；Merge 沿用一般驗證紀錄。Issue 不需計劃檢查。"
  if (verification) return "指派時只擷取獨立驗證：Epic 也略過強制計劃檢查；完成程式後仍須由獨立 checker 通過才可 Merge。"
  return "兩道 gate 都關閉：Epic 也略過強制計劃檢查；工作沿用一般執行與驗證流程。"
}
