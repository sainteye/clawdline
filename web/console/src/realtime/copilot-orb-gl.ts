// The voice copilot orb drawn with WebGL: a blue sphere of drifting cloud
// (layered noise warped by itself), a highlight, and a halo that grows with
// the voice. CopilotOrb.tsx falls back to CSS when this returns null.

import { orbPixelRatio, type OrbLook } from "./copilot-orb.js"

/**
 * The phase wraps at this length. Every speed the shader multiplies it by is a
 * whole number of twentieths, so each term repeats exactly here and the wrap
 * cannot be seen; the wrap keeps mobile GPUs' float precision intact.
 */
export const ORB_PHASE_PERIOD = Math.PI * 2 * 20

export interface OrbRenderer {
  draw(look: OrbLook, phase: number): void
  dispose(): void
}

const VERTEX = `
attribute vec2 aPos;
void main() { gl_Position = vec4(aPos, 0.0, 1.0); }
`

const FRAGMENT = `
#ifdef GL_FRAGMENT_PRECISION_HIGH
precision highp float;
#else
precision mediump float;
#endif
uniform vec2 uRes;
uniform float uPhase;
uniform float uSwell;
uniform float uGlow;
uniform float uBright;
uniform float uSwirl;

float hash(vec2 p) { return fract(sin(dot(p, vec2(127.1, 311.7))) * 43758.5453); }
float noise(vec2 p) {
  vec2 i = floor(p);
  vec2 f = fract(p);
  vec2 u = f * f * (3.0 - 2.0 * f);
  return mix(mix(hash(i), hash(i + vec2(1.0, 0.0)), u.x),
             mix(hash(i + vec2(0.0, 1.0)), hash(i + vec2(1.0, 1.0)), u.x), u.y);
}
float fbm(vec2 p) {
  float v = 0.0;
  float a = 0.5;
  mat2 m = mat2(1.6, 1.2, -1.2, 1.6);
  for (int k = 0; k < 4; k++) { v += a * noise(p); p = m * p; a *= 0.5; }
  return v;
}
mat2 rot(float a) { float c = cos(a); float s = sin(a); return mat2(c, -s, s, c); }

void main() {
  float side = min(uRes.x, uRes.y);
  vec2 uv = (gl_FragCoord.xy * 2.0 - uRes) / side;
  float r = 0.8 * uSwell;
  float d = length(uv);
  vec3 haloColor = vec3(0.38, 0.62, 0.98);
  // The halo fades out before the canvas's edge, so no square shows.
  float halo = exp(-pow(max(d - r, 0.0) * 5.0, 1.3)) * (1.0 - smoothstep(r, 0.98, d)) * uGlow * 0.6;
  if (d > r) { gl_FragColor = vec4(haloColor * halo, halo); return; }

  vec2 p = uv / r;
  float z = sqrt(max(1.0 - dot(p, p), 0.0));
  vec2 sp = p / (0.55 + 0.45 * z);
  vec2 q = rot(uPhase * 0.35) * sp * 1.3;
  q = rot(uSwirl * (1.0 - length(sp) * 0.6)) * q;
  vec2 w = vec2(
    fbm(q + 1.5 * vec2(cos(uPhase * 0.2), sin(uPhase * 0.2))),
    fbm(q + vec2(5.2, 1.3) + 1.5 * vec2(sin(uPhase * 0.15), cos(uPhase * 0.15))));
  float c = fbm(rot(-uPhase * 0.2) * q + 2.2 * w);

  float t = clamp(0.5 - p.y * 0.5, 0.0, 1.0);
  vec3 deep = vec3(0.11, 0.25, 0.62);
  vec3 mid = vec3(0.14, 0.40, 0.93);
  vec3 light = vec3(0.68, 0.85, 1.0);
  vec3 col = mix(deep, mid, smoothstep(0.0, 0.6, t));
  col = mix(col, light, smoothstep(0.55, 1.0, t));
  // Soft, thick mist rather than a planet's sharp clouds.
  float cloud = smoothstep(0.34, 0.86, c);
  col = mix(col, vec3(0.95, 0.98, 1.0), cloud * 0.8);
  col += smoothstep(0.6, 0.0, length(p - vec2(-0.32, 0.48))) * 0.32;
  col *= mix(0.7, 1.0, z) * uBright;

  float edge = smoothstep(r, r - 3.0 / side, d);
  gl_FragColor = vec4(mix(haloColor * halo, col, edge), max(edge, halo));
}
`

export function createOrbRenderer(canvas: HTMLCanvasElement): OrbRenderer | null {
  let gl: WebGLRenderingContext | null = null
  try {
    gl = canvas.getContext("webgl", { alpha: true, premultipliedAlpha: true, antialias: false, powerPreference: "low-power" }) as WebGLRenderingContext | null
  } catch {
    return null // refusal-ok: no WebGL here; the caller draws the CSS orb instead.
  }
  if (!gl) return null
  const program = link(gl)
  if (!program) return null

  const buffer = gl.createBuffer()
  gl.bindBuffer(gl.ARRAY_BUFFER, buffer)
  // One triangle that covers the whole canvas.
  gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1, -1, 3, -1, -1, 3]), gl.STATIC_DRAW)
  const position = gl.getAttribLocation(program, "aPos")
  gl.enableVertexAttribArray(position)
  gl.vertexAttribPointer(position, 2, gl.FLOAT, false, 0, 0)
  gl.useProgram(program)
  const at = (name: string) => gl!.getUniformLocation(program, name)
  const u = { res: at("uRes"), phase: at("uPhase"), swell: at("uSwell"), glow: at("uGlow"), bright: at("uBright"), swirl: at("uSwirl") }

  return {
    draw(look, phase) {
      const ratio = orbPixelRatio(window.devicePixelRatio)
      const width = Math.max(1, Math.round(canvas.clientWidth * ratio))
      const height = Math.max(1, Math.round(canvas.clientHeight * ratio))
      if (canvas.width !== width || canvas.height !== height) {
        canvas.width = width
        canvas.height = height
      }
      gl!.viewport(0, 0, width, height)
      gl!.clearColor(0, 0, 0, 0)
      gl!.clear(gl!.COLOR_BUFFER_BIT)
      gl!.uniform2f(u.res, width, height)
      gl!.uniform1f(u.phase, phase % ORB_PHASE_PERIOD)
      gl!.uniform1f(u.swell, look.swell)
      gl!.uniform1f(u.glow, look.glow)
      gl!.uniform1f(u.bright, look.bright)
      gl!.uniform1f(u.swirl, look.swirl)
      gl!.drawArrays(gl!.TRIANGLES, 0, 3)
    },
    dispose() {
      gl!.deleteBuffer(buffer)
      gl!.deleteProgram(program)
      gl!.getExtension("WEBGL_lose_context")?.loseContext()
    },
  }
}

function link(gl: WebGLRenderingContext): WebGLProgram | null {
  const vertex = compile(gl, gl.VERTEX_SHADER, VERTEX)
  const fragment = compile(gl, gl.FRAGMENT_SHADER, FRAGMENT)
  if (!vertex || !fragment) return null
  const program = gl.createProgram()
  if (!program) return null
  gl.attachShader(program, vertex)
  gl.attachShader(program, fragment)
  gl.linkProgram(program)
  gl.deleteShader(vertex)
  gl.deleteShader(fragment)
  if (!gl.getProgramParameter(program, gl.LINK_STATUS)) {
    console.warn("[Clawdline Copilot] orb shader did not link:", gl.getProgramInfoLog(program))
    gl.deleteProgram(program)
    return null
  }
  return program
}

function compile(gl: WebGLRenderingContext, type: number, source: string): WebGLShader | null {
  const shader = gl.createShader(type)
  if (!shader) return null
  gl.shaderSource(shader, source)
  gl.compileShader(shader)
  if (!gl.getShaderParameter(shader, gl.COMPILE_STATUS)) {
    console.warn("[Clawdline Copilot] orb shader did not compile:", gl.getShaderInfoLog(shader))
    gl.deleteShader(shader)
    return null
  }
  return shader
}
