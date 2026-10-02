import { useMutation, useQueryClient } from "@tanstack/react-query";
import { clsx } from "clsx";
import { Camera, ImagePlus, Minus, Plus, Trash2 } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { ApiError, avatarSrc, session } from "@/api/client";
import type { User } from "@/api/types";
import { Alert, Button, Dialog } from "@/components/ui";

/** A round profile picture, or the first letter of the name when there's none (USER-11). */
export function Avatar({
  name,
  url,
  className,
  children,
}: {
  name: string;
  url?: string | null;
  className?: string;
  children?: ReactNode;
}) {
  const [broken, setBroken] = useState<string | null>(null);
  const show = url && broken !== url;
  return (
    <span
      className={clsx(
        "relative flex shrink-0 items-center justify-center rounded-full bg-surface-3 font-bold",
        className,
      )}
    >
      {show ? (
        <img
          src={avatarSrc(url)}
          alt=""
          draggable={false}
          onError={() => setBroken(url)}
          className="size-full rounded-full object-cover"
        />
      ) : (
        name.slice(0, 1).toUpperCase()
      )}
      {children}
    </span>
  );
}

const FRAME = 256; // crop frame size in CSS pixels
const OUT = 512; // uploaded size; the server stores 512×512
const MAX_ZOOM = 4;

type View = { zoom: number; x: number; y: number }; // x, y: image top-left relative to the frame

async function send(
  method: "PUT" | "DELETE",
  userId: number,
  body?: Blob,
): Promise<User> {
  const res = await fetch(`/api/v1/users/${userId}/avatar`, {
    method,
    body,
    headers: {
      Authorization: `Bearer ${session.token}`,
      ...(body ? { "Content-Type": "application/octet-stream" } : {}),
    },
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok)
    throw new ApiError(
      res.status,
      data.code ?? "error",
      data.message ?? res.statusText,
    );
  return data as User;
}

function useAvatarMutation() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ userId, blob }: { userId: number; blob?: Blob }) =>
      send(blob ? "PUT" : "DELETE", userId, blob),
    onSuccess: (u) => {
      qc.setQueryData<User>(["me"], (me) => (me && me.id === u.id ? u : me));
      qc.invalidateQueries({ queryKey: ["users"] });
      qc.invalidateQueries({ queryKey: ["profiles"] });
    },
  });
}

/** Large avatar with change/remove buttons, for the Account page and the admin's Edit user dialog. */
export function AvatarPicker({ user }: { user: User }) {
  const input = useRef<HTMLInputElement>(null);
  const [file, setFile] = useState<string | null>(null);
  const remove = useAvatarMutation();
  const close = () => {
    if (file) URL.revokeObjectURL(file);
    setFile(null);
  };
  return (
    <div className="flex items-center gap-4">
      <button
        type="button"
        onClick={() => input.current?.click()}
        className="group relative rounded-full"
        aria-label="Choose a profile picture"
      >
        <Avatar
          name={user.displayName}
          url={user.avatarUrl}
          className="size-20 text-3xl"
        />
        <span className="absolute inset-0 flex items-center justify-center rounded-full bg-black/50 opacity-0 transition-opacity group-hover:opacity-100">
          <Camera className="size-6 text-white" aria-hidden />
        </span>
      </button>
      <div className="flex flex-wrap gap-2">
        <Button size="sm" onClick={() => input.current?.click()}>
          <ImagePlus className="size-4" aria-hidden />{" "}
          {user.avatarUrl ? "Change picture" : "Add picture"}
        </Button>
        {user.avatarUrl && (
          <Button
            size="sm"
            variant="ghost"
            loading={remove.isPending}
            onClick={() => remove.mutate({ userId: user.id })}
          >
            <Trash2 className="size-4" aria-hidden /> Remove
          </Button>
        )}
      </div>
      {remove.isError && (
        <span className="text-sm text-danger">{remove.error.message}</span>
      )}
      <input
        ref={input}
        type="file"
        accept="image/*"
        className="hidden"
        onChange={(e) => {
          const f = e.target.files?.[0];
          e.target.value = "";
          if (f) setFile(URL.createObjectURL(f));
        }}
      />
      {file && <AvatarEditor src={file} user={user} onClose={close} />}
    </div>
  );
}

/** Lets the user drag and zoom a picture inside the round frame used for profile bubbles. */
function AvatarEditor({
  src,
  user,
  onClose,
}: {
  src: string;
  user: User;
  onClose: () => void;
}) {
  const [img, setImg] = useState<HTMLImageElement | null>(null);
  const [loadError, setLoadError] = useState(false);
  const [view, setView] = useState<View>({ zoom: 1, x: 0, y: 0 });
  const drag = useRef<{ px: number; py: number; x: number; y: number } | null>(
    null,
  );
  const [dragging, setDragging] = useState(false);
  const save = useAvatarMutation();

  useEffect(() => {
    const i = new Image();
    i.onload = () => {
      const s = FRAME / Math.min(i.naturalWidth, i.naturalHeight);
      setView({
        zoom: 1,
        x: (FRAME - i.naturalWidth * s) / 2,
        y: (FRAME - i.naturalHeight * s) / 2,
      });
      setImg(i);
    };
    i.onerror = () => setLoadError(true);
    i.src = src;
  }, [src]);

  const base = img ? FRAME / Math.min(img.naturalWidth, img.naturalHeight) : 1;
  const scale = base * view.zoom;
  // Keep the frame covered by the image.
  const clamp = (v: View): View => {
    if (!img) return v;
    const s = base * v.zoom;
    return {
      zoom: v.zoom,
      x: Math.min(0, Math.max(FRAME - img.naturalWidth * s, v.x)),
      y: Math.min(0, Math.max(FRAME - img.naturalHeight * s, v.y)),
    };
  };
  // Zoom around the frame's center.
  const zoomTo = (z: number) =>
    setView((v) => {
      const zoom = Math.min(MAX_ZOOM, Math.max(1, z));
      const k = zoom / v.zoom;
      const c = FRAME / 2;
      return clamp({ zoom, x: c - (c - v.x) * k, y: c - (c - v.y) * k });
    });

  const endDrag = () => {
    drag.current = null;
    setDragging(false);
  };

  const export_ = () =>
    new Promise<Blob>((resolve, reject) => {
      if (!img) return reject(new Error("no image"));
      const canvas = document.createElement("canvas");
      canvas.width = canvas.height = OUT;
      const ctx = canvas.getContext("2d")!;
      ctx.fillStyle = "#fff";
      ctx.fillRect(0, 0, OUT, OUT);
      ctx.imageSmoothingQuality = "high";
      ctx.drawImage(
        img,
        -view.x / scale,
        -view.y / scale,
        FRAME / scale,
        FRAME / scale,
        0,
        0,
        OUT,
        OUT,
      );
      canvas.toBlob(
        (b) =>
          b ? resolve(b) : reject(new Error("couldn't encode the picture")),
        "image/jpeg",
        0.9,
      );
    });

  return (
    <Dialog
      open
      onClose={onClose}
      title="Adjust picture"
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button
            variant="primary"
            disabled={!img}
            loading={save.isPending}
            onClick={async () =>
              save.mutate(
                { userId: user.id, blob: await export_() },
                { onSuccess: onClose },
              )
            }
          >
            Save
          </Button>
        </>
      }
    >
      <div className="space-y-5">
        {loadError && (
          <Alert tone="error">
            This file isn't an image this browser can open.
          </Alert>
        )}
        {save.isError && <Alert tone="error">{save.error.message}</Alert>}
        <p className="text-center text-sm text-muted">
          Drag to position. Zoom with the slider or scroll wheel.
        </p>
        <div
          className="relative mx-auto touch-none overflow-hidden rounded-lg bg-black select-none"
          style={{
            width: FRAME,
            height: FRAME,
            cursor: dragging ? "grabbing" : "grab",
          }}
          tabIndex={0}
          aria-label="Picture position. Use arrow keys to move and plus or minus to zoom."
          onPointerDown={(e) => {
            e.currentTarget.setPointerCapture(e.pointerId);
            drag.current = {
              px: e.clientX,
              py: e.clientY,
              x: view.x,
              y: view.y,
            };
            setDragging(true);
          }}
          onPointerMove={(e) => {
            const d = drag.current;
            if (d)
              setView((v) =>
                clamp({
                  ...v,
                  x: d.x + e.clientX - d.px,
                  y: d.y + e.clientY - d.py,
                }),
              );
          }}
          onPointerUp={endDrag}
          onPointerCancel={endDrag}
          onWheel={(e) => zoomTo(view.zoom * (e.deltaY < 0 ? 1.1 : 1 / 1.1))}
          onKeyDown={(e) => {
            const step = 8;
            const moves: Record<string, [number, number]> = {
              ArrowLeft: [step, 0],
              ArrowRight: [-step, 0],
              ArrowUp: [0, step],
              ArrowDown: [0, -step],
            };
            const m = moves[e.key];
            if (m) {
              e.preventDefault();
              setView((v) => clamp({ ...v, x: v.x + m[0], y: v.y + m[1] }));
            } else if (e.key === "+" || e.key === "=") zoomTo(view.zoom * 1.1);
            else if (e.key === "-") zoomTo(view.zoom / 1.1);
          }}
        >
          {img && (
            <img
              src={src}
              alt=""
              draggable={false}
              className="pointer-events-none absolute top-0 left-0 max-w-none origin-top-left"
              style={{
                width: img.naturalWidth,
                height: img.naturalHeight,
                transform: `translate(${view.x}px, ${view.y}px) scale(${scale})`,
              }}
            />
          )}
          {/* Dim everything outside the circle that profile bubbles show. */}
          <div className="pointer-events-none absolute inset-0 rounded-full shadow-[0_0_0_9999px_rgba(0,0,0,0.6)] ring-2 ring-white/80" />
        </div>
        <div className="mx-auto flex max-w-64 items-center gap-3">
          <button
            type="button"
            onClick={() => zoomTo(view.zoom / 1.2)}
            className="rounded p-1 text-muted hover:text-text"
            aria-label="Zoom out"
          >
            <Minus className="size-4" />
          </button>
          <input
            type="range"
            min={1}
            max={MAX_ZOOM}
            step={0.01}
            value={view.zoom}
            onChange={(e) => zoomTo(Number(e.target.value))}
            aria-label="Zoom"
            className="flex-1 accent-[var(--color-accent)]"
          />
          <button
            type="button"
            onClick={() => zoomTo(view.zoom * 1.2)}
            className="rounded p-1 text-muted hover:text-text"
            aria-label="Zoom in"
          >
            <Plus className="size-4" />
          </button>
        </div>
      </div>
    </Dialog>
  );
}
