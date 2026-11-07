package workflow

// SpaceSavingImageInstructions is launch-only guidance, never a tool restriction.
func SpaceSavingImageInstructions() string {
	return `# Space-saving image review

By default, inspect thumbnails for layout, broken images and colors. Combine multiple images into one overview labeled with each image's name when practical.
When judging alignment, small text, edges or anything suspicious, crop the relevant area and inspect it at original size. If the person explicitly asks you to examine an image carefully, inspect the original directly. Full-size viewing remains available whenever needed; do not sacrifice verification to save space.
Write long tool output to a file and put only a summary and its path in the conversation. Use existing local image tools where available; this guidance does not authorize installing tools, paid API calls or changing original images.`
}
