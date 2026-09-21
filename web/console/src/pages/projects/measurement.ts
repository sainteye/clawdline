/** The words replacing the copied Projects page's measured-zero sentence. */
export function projectFeatureMeasurementWords(chinese: boolean): { empty: string; read: string } {
  return chinese
    ? {
        empty: "這個 daemon 尚未量測 Feature 歸屬，因此不知道這個專案有沒有完成過 Feature。",
        read: "掃描了 {rows} 列 · 這個專案 {project} 列 · worktree 內 {worktree} 列 · Feature 歸屬：未量測",
      }
    : {
        empty: "This daemon does not measure Feature attribution yet, so it is not known whether this project completed any Features.",
        read: "Read {rows} rows · {project} under this Project · {worktree} in a worktree · Feature attribution: not measured",
      }
}
