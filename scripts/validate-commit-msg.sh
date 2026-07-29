#!/bin/sh

first_line="$(sed -n '1p' "$1" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')"

if [ -z "$first_line" ]; then
  echo "Commit message cannot be empty."
  exit 1
fi

case "$first_line" in
  "Merge "*|"Revert "*|"fixup!"*|"squash!"*)
    exit 0
    ;;
esac

pattern='^(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert)(\([a-z0-9][a-z0-9._/-]*\))?(!)?: .+$'

if ! printf '%s\n' "$first_line" | grep -E -q "$pattern"; then
  echo "Invalid commit message format."
  echo "Expected: <type>(<scope>)!: <icon> <subject>"
  echo "Allowed <type>: feat, fix, docs, style, refactor, perf, test, build, ci, chore, revert"
  echo "Examples:"
  echo "  feat(auth): ✨ add session refresh endpoint"
  echo "  fix!: 🐛 remove deprecated browser runtime path"
  echo "  chore(ci): 🔧 pin python version in workflow"
  echo "Your message: $first_line"
  exit 1
fi

commit_type="${first_line%%[(!:]*}"

case "$commit_type" in
  feat) expected_icon="✨" ;;
  fix) expected_icon="🐛" ;;
  docs) expected_icon="📝" ;;
  style) expected_icon="💄" ;;
  refactor) expected_icon="♻️" ;;
  perf) expected_icon="⚡" ;;
  test) expected_icon="✅" ;;
  build) expected_icon="📦" ;;
  ci) expected_icon="👷" ;;
  chore) expected_icon="🔖" ;;
  revert) expected_icon="⏪" ;;
esac

subject="${first_line#*: }"

if ! printf '%s\n' "$subject" | grep -E -q "^${expected_icon} .+"; then
  echo "Invalid commit icon for type: $commit_type"
  echo "Expected subject to start with: ${expected_icon} "
  echo "Example:"
  echo "  ${commit_type}: ${expected_icon} your subject here"
  echo "Your message: $first_line"
  exit 1
fi
