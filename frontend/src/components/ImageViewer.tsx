import { useEffect, useCallback } from "react";

type ImageViewerProps = {
  src: string;
  alt: string;
  onClose: () => void;
};

export function ImageViewer({ src, alt, onClose }: ImageViewerProps) {
  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        onClose();
      }
    },
    [onClose],
  );

  useEffect(() => {
    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [handleKeyDown]);

  return (
    <div
      className="ms-image-viewer"
      role="dialog"
      aria-label={alt}
      onClick={onClose}
    >
      <button
        className="ms-image-viewer__close"
        onClick={onClose}
        aria-label="关闭图片"
        type="button"
      >
        <svg
          aria-hidden="true"
          fill="none"
          focusable="false"
          strokeLinecap="round"
          strokeLinejoin="round"
          viewBox="0 0 24 24"
          width="24"
          height="24"
        >
          <line x1="18" y1="6" x2="6" y2="18" stroke="currentColor" strokeWidth="2" />
          <line x1="6" y1="6" x2="18" y2="18" stroke="currentColor" strokeWidth="2" />
        </svg>
      </button>
      <img
        className="ms-image-viewer__img"
        src={src}
        alt={alt}
        onClick={(e) => e.stopPropagation()}
        onDoubleClick={(e) => e.stopPropagation()}
      />
    </div>
  );
}
