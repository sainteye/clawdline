/** The preview preserves the selected machine's separator, including roots. */
export function projectFolderPath(parent: string, name: string): string {
  const separator = parent.includes("\\") ? "\\" : "/"
  return parent.replace(/[\\/]+$/, "") + separator + name
}

export function validProjectFolderName(name: string): boolean {
  return !!name.trim() && name !== "." && name !== ".." && !/[\\/\u0000-\u001f\u007f-\u009f]/.test(name)
}
