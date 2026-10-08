/**
 * One ordinary Run now, followed by the person's explicit force retry only
 * when the machine says this same schedule is already active.
 */
export async function runScheduleWithForce<T>(
  run: (force: boolean) => Promise<T>,
  confirmForce: () => boolean,
): Promise<T> {
  try {
    return await run(false)
  } catch (error) {
    const code = error && typeof error === "object" && "code" in error ? String(error.code || "") : ""
    if (code !== "schedule_active" || !confirmForce()) throw error
    return run(true)
  }
}
