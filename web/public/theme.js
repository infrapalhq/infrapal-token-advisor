// Sets the colour theme before the page draws, so it doesn't flash. It's a file
// rather than inline in index.html so the content security policy can forbid
// inline scripts.
try {
  document.documentElement.dataset.theme = localStorage.getItem("ta-theme") === "light" ? "light" : "dark";
  // Keys an earlier build wrote. Nothing reads them now, and a stale
  // infrapal:theme used to contradict ta-theme.
  localStorage.removeItem("infrapal:theme");
  localStorage.removeItem("infrapal:range");
} catch {
  document.documentElement.dataset.theme = "dark";
}
