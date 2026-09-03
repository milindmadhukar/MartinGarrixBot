// Minimal drag-to-pan, slider-to-zoom cropper for the backgrounds upload
// page. No dependency: the rest of the dashboard is server-rendered HTML
// plus htmx, and this is the one place that needs actual canvas work.
//
// The rank card is a fixed 1000x300 canvas (see utils/levelling.go), so the
// crop viewport locks to that aspect ratio and the exported PNG is always
// exactly 1000x300 -- the server re-encodes it but never resizes it.
(function () {
  "use strict";

  const CARD_W = 1000;
  const CARD_H = 300;

  const sourceInput = document.getElementById("source-file");
  const stage = document.getElementById("crop-stage");
  const viewport = document.getElementById("crop-viewport");
  const img = document.getElementById("crop-img");
  const zoom = document.getElementById("crop-zoom");
  const croppedInput = document.getElementById("cropped-file");
  const form = document.getElementById("upload-form");
  const submitBtn = document.getElementById("upload-submit");
  if (!sourceInput || !form) return;

  let naturalW = 0;
  let naturalH = 0;
  let baseScale = 1;
  let scale = 1;
  let offsetX = 0;
  let offsetY = 0;
  let dragging = false;
  let dragStartX = 0;
  let dragStartY = 0;
  let dragOriginX = 0;
  let dragOriginY = 0;

  function applyTransform() {
    img.style.width = naturalW * scale + "px";
    img.style.height = naturalH * scale + "px";
    img.style.transform = "translate(" + offsetX + "px, " + offsetY + "px)";
  }

  function clampOffset() {
    const vp = viewport.getBoundingClientRect();
    const w = naturalW * scale;
    const h = naturalH * scale;
    const minX = Math.min(0, vp.width - w);
    const minY = Math.min(0, vp.height - h);
    offsetX = Math.max(minX, Math.min(0, offsetX));
    offsetY = Math.max(minY, Math.min(0, offsetY));
  }

  sourceInput.addEventListener("change", function () {
    const file = sourceInput.files[0];
    if (!file) return;

    img.onload = function () {
      naturalW = img.naturalWidth;
      naturalH = img.naturalHeight;

      // Unhide the stage before measuring the viewport: getBoundingClientRect()
      // on a descendant of a `hidden` element returns 0x0, which previously made
      // every scale/offset below compute against a zero-size box -- the image
      // loaded successfully but rendered at 0x0, invisible in a viewport that
      // otherwise looked fine.
      stage.hidden = false;

      const vp = viewport.getBoundingClientRect();
      // "cover" fit: the smaller edge fills the viewport, the larger one
      // overflows and is what dragging reveals.
      baseScale = Math.max(vp.width / naturalW, vp.height / naturalH);
      scale = baseScale;
      offsetX = (vp.width - naturalW * scale) / 2;
      offsetY = (vp.height - naturalH * scale) / 2;

      zoom.value = "1";
      applyTransform();
      submitBtn.disabled = false;
    };
    img.src = URL.createObjectURL(file);
  });

  zoom.addEventListener("input", function () {
    const factor = parseFloat(zoom.value) || 1;
    scale = baseScale * factor;
    clampOffset();
    applyTransform();
  });

  viewport.addEventListener("pointerdown", function (e) {
    if (!naturalW) return;
    dragging = true;
    dragStartX = e.clientX;
    dragStartY = e.clientY;
    dragOriginX = offsetX;
    dragOriginY = offsetY;
    viewport.setPointerCapture(e.pointerId);
  });

  viewport.addEventListener("pointermove", function (e) {
    if (!dragging) return;
    offsetX = dragOriginX + (e.clientX - dragStartX);
    offsetY = dragOriginY + (e.clientY - dragStartY);
    clampOffset();
    applyTransform();
  });

  ["pointerup", "pointercancel", "pointerleave"].forEach(function (evt) {
    viewport.addEventListener(evt, function () {
      dragging = false;
    });
  });

  form.addEventListener("submit", function (e) {
    if (!naturalW) return; // no image chosen: let the browser's own validation handle it

    e.preventDefault();

    const vp = viewport.getBoundingClientRect();
    const exportScale = CARD_W / vp.width;

    const canvas = document.createElement("canvas");
    canvas.width = CARD_W;
    canvas.height = CARD_H;
    const out = canvas.getContext("2d");
    out.drawImage(
      img,
      0, 0, naturalW, naturalH,
      offsetX * exportScale, offsetY * exportScale,
      naturalW * scale * exportScale, naturalH * scale * exportScale
    );

    canvas.toBlob(function (blob) {
      const file = new File([blob], "background.png", { type: "image/png" });
      const dt = new DataTransfer();
      dt.items.add(file);
      croppedInput.files = dt.files;
      form.submit();
    }, "image/png");
  });
})();
