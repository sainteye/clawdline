/**
 * The common closeability state is an explanation, not an error dump. Keep this decision pure so
 * the confirmation sheet cannot quietly regress to making a person interpret a broker code.
 */
export function closeabilityHelpModel(projected, strings) {
    if (!projected || projected.state !== "needs_attestation" ||
        !Array.isArray(projected.reasons) ||
        !projected.reasons.some(function (reason) { return reason.kind === "attestation"; })) {
        return null;
    }
    return {
        explanation: strings.closeabilityAttestationExplanation,
        detailsLabel: strings.closeabilityTechnicalDetails,
        cancelLabel: strings.webReviewBeforeClosing,
        confirmLabel: strings.webConfirmEndAnyway
    };
}
