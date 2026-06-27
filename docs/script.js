/* go-bus landing page — interactions */
(function () {
  "use strict";

  // ── nav: tint + blur once the page scrolls past the top ──
  var nav = document.getElementById("nav");
  function onScroll() {
    if (nav) nav.classList.toggle("scrolled", window.scrollY > 8);
  }
  window.addEventListener("scroll", onScroll, { passive: true });
  onScroll();

  // ── reveal: stagger sections in as they enter the viewport ──
  var reveals = document.querySelectorAll(".reveal");
  if ("IntersectionObserver" in window) {
    var io = new IntersectionObserver(function (entries) {
      entries.forEach(function (e) {
        if (e.isIntersecting) {
          e.target.classList.add("in");
          io.unobserve(e.target);
        }
      });
    }, { threshold: 0.12, rootMargin: "0px 0px -8% 0px" });
    reveals.forEach(function (el) { io.observe(el); });
  } else {
    reveals.forEach(function (el) { el.classList.add("in"); });
  }

  // ── code copy ──
  var copyBtn = document.getElementById("copy");
  if (copyBtn) {
    var codeEl = copyBtn.closest(".code").querySelector("code");
    copyBtn.addEventListener("click", function () {
      var done = function () {
        copyBtn.textContent = "copied ✓";
        copyBtn.classList.add("copied");
        setTimeout(function () {
          copyBtn.textContent = "copy";
          copyBtn.classList.remove("copied");
        }, 1600);
      };
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(codeEl.innerText).then(done).catch(function () {
          copyBtn.textContent = "select + ⌘C";
        });
      } else {
        copyBtn.textContent = "select + ⌘C";
      }
    });
  }
})();
