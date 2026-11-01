// The rules for the commit messages of a pull request, checked by .github/workflows/commits.yaml
export default {
  extends: ['@commitlint/config-conventional'],
  rules: {
    // Renovate writes long lines in the body and the footer, and the summary is the whole message of the rest
    'body-max-line-length': [0],
    'footer-max-line-length': [0],
    'footer-leading-blank': [0],
    'header-max-length': [2, 'always', 120],
  },
}
