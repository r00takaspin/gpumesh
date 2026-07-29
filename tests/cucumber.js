module.exports = {
  default: {
    paths: ["features/**/*.feature"],
    require: ["steps/**/*.js", "support/**/*.js"],
    format: ["progress", "html:cucumber-report.html"],
    defaultTimeout: 30000
  },
  api: {
    paths: ["features/api/**/*.feature"],
    require: ["steps/api.steps.js", "support/**/*.js"],
    format: ["progress"],
    defaultTimeout: 30000
  },
  ui: {
    paths: ["features/ui/**/*.feature"],
    require: ["steps/ui.steps.js", "support/**/*.js"],
    format: ["progress"],
    defaultTimeout: 30000
  }
};
