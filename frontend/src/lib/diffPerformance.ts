const MERGE_EDITOR_MAX_LINES = 1_500
const MERGE_EDITOR_MAX_CHARS = 120_000

function lineCount(value: string) {
  if (!value) return 0
  return value.split('\n').length
}

export function shouldUseMergeEditor(oldYaml: string, newYaml: string) {
  const totalChars = oldYaml.length + newYaml.length
  const maxLines = Math.max(lineCount(oldYaml), lineCount(newYaml))
  return totalChars <= MERGE_EDITOR_MAX_CHARS && maxLines <= MERGE_EDITOR_MAX_LINES
}
