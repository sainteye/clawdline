/** Pure selection arithmetic for the composer skill menu. */
export function clampSkillPickerIndex(index, count) {
    var last = Math.max(0, Math.floor(Number(count) || 0) - 1);
    var value = Number.isInteger(index) ? index : 0;
    return Math.max(0, Math.min(value, last));
}

export function selectedSkill(matches, index) {
    var rows = Array.isArray(matches) ? matches : [];
    if (!rows.length) return null;
    return rows[clampSkillPickerIndex(index, rows.length)] || null;
}
