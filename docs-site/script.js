// Fade-and-rise reveal for any .reveal element as it scrolls into view.
// Shared by index.html and details.html.
const io = new IntersectionObserver(
  entries => entries.forEach(e => { if (e.isIntersecting) { e.target.classList.add("in"); io.unobserve(e.target); } }),
  { threshold: 0.12 }
);
document.querySelectorAll(".reveal").forEach(el => io.observe(el));
