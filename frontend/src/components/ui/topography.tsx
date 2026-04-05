import { type ReactNode, useEffect, useRef } from "react";

import { cn } from "@/lib/utils";

export interface TopographyBackgroundProps {
  className?: string;
  children?: ReactNode;
  lineCount?: number;
  lineColor?: string;
  backgroundColor?: string;
  speed?: number;
  strokeWidth?: number;
}

export function TopographyBackground({
  className,
  children,
  lineCount = 20,
  lineColor = "rgba(120, 120, 120, 0.3)",
  backgroundColor = "#0a0a0f",
  speed = 1,
  strokeWidth = 1,
}: TopographyBackgroundProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const canvas = canvasRef.current;
    const container = containerRef.current;
    if (!canvas || !container) {
      return;
    }

    const context = canvas.getContext("2d");
    if (!context) {
      return;
    }

    const dpr = window.devicePixelRatio || 1;
    let width = 0;
    let height = 0;
    let animationId = 0;
    let tick = 0;

    const resizeCanvas = () => {
      const rect = container.getBoundingClientRect();
      width = rect.width;
      height = rect.height;

      canvas.width = width * dpr;
      canvas.height = height * dpr;
      canvas.style.width = `${width}px`;
      canvas.style.height = `${height}px`;
      context.setTransform(dpr, 0, 0, dpr, 0, 0);
    };

    const getHeight = (x: number, time: number) => {
      const scale = 0.003;

      return (
        Math.sin(x * scale * 2 + time) * 30 +
        Math.sin(x * scale * 3.7 + time * 0.7) * 20 +
        Math.sin(x * scale * 1.3 - time * 0.5) * 40 +
        Math.sin(x * scale * 5.1 + time * 1.2) * 10 +
        Math.sin(x * scale * 0.7 + time * 0.3) * 50
      );
    };

    const animate = () => {
      tick += 0.008 * speed;

      context.fillStyle = backgroundColor;
      context.fillRect(0, 0, width, height);

      context.strokeStyle = lineColor;
      context.lineWidth = strokeWidth;
      context.lineCap = "round";
      context.lineJoin = "round";

      const spacing = height / Math.max(lineCount - 1, 1);
      const padding = 50;

      for (let index = 0; index < lineCount; index += 1) {
        const baseY = spacing * index;
        context.beginPath();

        let started = false;
        for (let x = -padding; x <= width + padding; x += 3) {
          const terrainHeight = getHeight(x + index * 100, tick);
          const y = baseY + terrainHeight;

          if (!started) {
            context.moveTo(x, y);
            started = true;
          } else {
            context.lineTo(x, y);
          }
        }

        context.stroke();
      }

      animationId = window.requestAnimationFrame(animate);
    };

    resizeCanvas();
    const resizeObserver = new ResizeObserver(resizeCanvas);
    resizeObserver.observe(container);
    animationId = window.requestAnimationFrame(animate);

    return () => {
      window.cancelAnimationFrame(animationId);
      resizeObserver.disconnect();
    };
  }, [backgroundColor, lineColor, lineCount, speed, strokeWidth]);

  return (
    <div
      ref={containerRef}
      className={cn("fixed inset-0 overflow-hidden", className)}
      style={{ backgroundColor }}
    >
      <canvas ref={canvasRef} className="absolute inset-0 h-full w-full" />

      <div
        className="pointer-events-none absolute inset-0 opacity-50"
        style={{
          background: `radial-gradient(ellipse at 50% 50%, transparent 0%, ${backgroundColor} 100%)`,
        }}
      />

      <div
        className="pointer-events-none absolute inset-0"
        style={{
          background: `radial-gradient(ellipse at center, transparent 0%, transparent 40%, ${backgroundColor} 100%)`,
        }}
      />

      {children ? <div className="relative z-10 h-full w-full">{children}</div> : null}
    </div>
  );
}
