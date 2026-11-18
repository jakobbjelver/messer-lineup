/**
 * The lamps.
 *
 * This is the reactbits LightRays shader (the estate's own copy lives in `messer-swipe`), ported to raw WebGL the
 * way the design mockup proves it, with the two changes the stage needs:
 *
 *   1. **Two origins.** A rig has a warm key from the left and a magenta fill from the right; one origin is a
 *      sunburst, not a stage.
 *   2. **`lightSpread` ≈ .085** (exponent ≈ 11.7). The default 1.0 is a cosine falloff that reads as "a blur";
 *      shafts need the tighter exponent.
 *
 * Two traps are load-bearing in the fragment shader below: alpha is `max(r,g,b) * 1.25` (with `1.0` the
 * light-mode `multiply` pass paints the page black), and the colour comes from the CSS tokens via a 1×1 canvas
 * probe, so the lamps retint with the theme instead of being hard-coded.
 */

const VERT = `attribute vec2 position; varying vec2 vUv;
  void main(){ vUv = position * 0.5 + 0.5; gl_Position = vec4(position, 0.0, 1.0); }`;

const FRAG = `precision highp float;
  uniform float iTime; uniform vec2 iResolution;
  uniform vec2 rayPos; uniform vec2 rayDir; uniform vec3 raysColor;
  uniform vec2 rayPos2; uniform vec2 rayDir2; uniform vec3 fillColor;
  uniform float raysSpeed; uniform float lightSpread; uniform float rayLength;
  uniform float pulsating; uniform float fadeDistance; uniform float saturation;
  uniform vec2 mousePos; uniform float mouseInfluence; uniform float noiseAmount;
  uniform float distortion; uniform float exposure;
  varying vec2 vUv;
  float noise(vec2 st){ return fract(sin(dot(st.xy, vec2(12.9898,78.233))) * 43758.5453123); }
  float rayStrength(vec2 raySource, vec2 rayRefDirection, vec2 coord, float seedA, float seedB, float speed){
    vec2 sourceToCoord = coord - raySource;
    vec2 dirNorm = normalize(sourceToCoord);
    float cosAngle = dot(dirNorm, rayRefDirection);
    float distortedAngle = cosAngle + distortion * sin(iTime * 0.02 + length(sourceToCoord) * 0.01) * 0.2;
    float spreadFactor = pow(max(distortedAngle, 0.0), 1.0 / max(lightSpread, 0.001));
    float distance = length(sourceToCoord);
    float maxDistance = iResolution.x * rayLength;
    float lengthFalloff = clamp((maxDistance - distance) / maxDistance, 0.0, 1.0);
    float fadeFalloff = clamp((iResolution.x * fadeDistance - distance) / (iResolution.x * fadeDistance), 0.35, 1.0);
    float pulse = pulsating > 0.5 ? (0.8 + 0.2 * sin(iTime * speed * 3.0)) : 1.0;
    float baseStrength = clamp(
      (0.45 + 0.15 * sin(distortedAngle * seedA + iTime * speed)) +
      (0.3 + 0.2 * cos(-distortedAngle * seedB + iTime * speed)), 0.0, 1.0);
    return baseStrength * lengthFalloff * fadeFalloff * spreadFactor * pulse;
  }
  void main(){
    vec2 fragCoord = vUv * iResolution.xy;
    vec2 coord = vec2(fragCoord.x, iResolution.y - fragCoord.y);
    vec2 dir1 = rayDir, dir2 = rayDir2;
    if (mouseInfluence > 0.0){
      vec2 mp = mousePos * iResolution.xy;
      dir1 = normalize(mix(rayDir,  normalize(mp - rayPos),  mouseInfluence));
      dir2 = normalize(mix(rayDir2, normalize(mp - rayPos2), mouseInfluence * 0.65));
    }
    float key = rayStrength(rayPos,  dir1, coord, 36.2214, 21.11349, 1.5  * raysSpeed) * 0.62
              + rayStrength(rayPos,  dir1, coord, 22.3991, 18.0234,  1.1  * raysSpeed) * 0.48;
    float fill = rayStrength(rayPos2, dir2, coord, 31.7712, 19.2261, 1.25 * raysSpeed) * 0.44
              + rayStrength(rayPos2, dir2, coord, 17.3319, 24.1177, 0.90 * raysSpeed) * 0.34;
    vec3 rgb = key * raysColor + fill * fillColor;
    if (noiseAmount > 0.0){
      float n = noise(coord * 0.008 + iTime * 0.002);
      rgb *= (1.0 - noiseAmount + noiseAmount * n);
    }
    float brightness = 1.0 - (coord.y / iResolution.y);
    rgb *= vec3(0.55 + brightness * 0.45, 0.65 + brightness * 0.35, 0.8 + brightness * 0.2);
    if (saturation != 1.0){
      float gray = dot(rgb, vec3(0.299, 0.587, 0.114));
      rgb = mix(vec3(gray), rgb, saturation);
    }
    rgb *= exposure;
    gl_FragColor = vec4(rgb, clamp(max(max(rgb.r, rgb.g), rgb.b) * 1.25, 0.0, 1.0));
  }`;

/** Reads a CSS colour token into linear-ish [0,1] RGB by painting one pixel and reading it back. */
function readToken(probe: CanvasRenderingContext2D, token: string): [number, number, number] {
  const value = getComputedStyle(document.documentElement).getPropertyValue(token).trim();
  probe.fillStyle = "#000";
  probe.fillRect(0, 0, 1, 1);
  probe.fillStyle = value || "#fff";
  probe.fillRect(0, 0, 1, 1);
  const d = probe.getImageData(0, 0, 1, 1).data;
  return [d[0] / 255, d[1] / 255, d[2] / 255];
}

export type Lamps = { boot(): void; stop(): void; pointer(x: number, y: number): void; retint(): void };

export function createLamps(canvas: HTMLCanvasElement): Lamps {
  let gl: WebGLRenderingContext | null = null;
  let program: WebGLProgram | null = null;
  const uniforms: Record<string, WebGLUniformLocation | null> = {};
  let raf = 0;
  let running = false;
  let startedAt = 0;
  const mouse: [number, number] = [0.5, 0.15];
  const smooth: [number, number] = [0.5, 0.15];
  let tint: [number, number, number] = [1, 1, 1];
  let fill: [number, number, number] = [1, 1, 1];

  const probe = document.createElement("canvas").getContext("2d", { willReadFrequently: true });

  function init(): boolean {
    gl = canvas.getContext("webgl", { alpha: true, antialias: false, premultipliedAlpha: false });
    if (!gl) return false;
    const vs = gl.createShader(gl.VERTEX_SHADER)!;
    const fs = gl.createShader(gl.FRAGMENT_SHADER)!;
    gl.shaderSource(vs, VERT);
    gl.compileShader(vs);
    gl.shaderSource(fs, FRAG);
    gl.compileShader(fs);
    if (!gl.getShaderParameter(fs, gl.COMPILE_STATUS)) {
      console.error("lamps: fragment shader", gl.getShaderInfoLog(fs));
      return false;
    }
    program = gl.createProgram()!;
    gl.attachShader(program, vs);
    gl.attachShader(program, fs);
    gl.linkProgram(program);
    gl.useProgram(program);
    const buffer = gl.createBuffer();
    gl.bindBuffer(gl.ARRAY_BUFFER, buffer);
    gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1, -1, 3, -1, -1, 3]), gl.STATIC_DRAW);
    const loc = gl.getAttribLocation(program, "position");
    gl.enableVertexAttribArray(loc);
    gl.vertexAttribPointer(loc, 2, gl.FLOAT, false, 0, 0);
    for (const name of [
      "iTime",
      "iResolution",
      "rayPos",
      "rayDir",
      "raysColor",
      "rayPos2",
      "rayDir2",
      "fillColor",
      "raysSpeed",
      "lightSpread",
      "rayLength",
      "pulsating",
      "fadeDistance",
      "saturation",
      "mousePos",
      "mouseInfluence",
      "noiseAmount",
      "distortion",
      "exposure",
    ]) {
      uniforms[name] = gl.getUniformLocation(program, name);
    }
    return true;
  }

  function resize() {
    if (!gl) return;
    const dpr = Math.min(window.devicePixelRatio || 1, 2);
    canvas.width = Math.round(window.innerWidth * dpr);
    canvas.height = Math.round(window.innerHeight * dpr);
    gl.viewport(0, 0, canvas.width, canvas.height);
  }

  function frame(now: number) {
    if (!running || !gl) return;
    const t = (now - startedAt) / 1000;
    smooth[0] += (mouse[0] - smooth[0]) * 0.015;
    smooth[1] += (mouse[1] - smooth[1]) * 0.015;
    gl.uniform1f(uniforms.iTime, t);
    gl.uniform2f(uniforms.iResolution, canvas.width, canvas.height);
    gl.uniform2f(uniforms.rayPos, canvas.width * 0.21, -canvas.height * 0.3);
    gl.uniform2f(uniforms.rayDir, 0.1, 1);
    gl.uniform2f(uniforms.rayPos2, canvas.width * 0.8, -canvas.height * 0.36);
    gl.uniform2f(uniforms.rayDir2, -0.26, 1);
    gl.uniform3fv(uniforms.raysColor, tint);
    gl.uniform3fv(uniforms.fillColor, fill);
    gl.uniform1f(uniforms.raysSpeed, 0.03); // almost still: the lamp breathes, it does not sweep
    gl.uniform1f(uniforms.lightSpread, 0.085);
    gl.uniform1f(uniforms.rayLength, 1.25);
    gl.uniform1f(uniforms.pulsating, 0);
    gl.uniform1f(uniforms.fadeDistance, 0.62);
    gl.uniform1f(uniforms.saturation, 1.0);
    gl.uniform2f(uniforms.mousePos, smooth[0] * canvas.width, smooth[1] * canvas.height);
    gl.uniform1f(uniforms.mouseInfluence, 0.18);
    gl.uniform1f(uniforms.noiseAmount, 0);
    gl.uniform1f(uniforms.distortion, 0.16);
    gl.uniform1f(uniforms.exposure, 2.5);
    gl.drawArrays(gl.TRIANGLES, 0, 3);
    raf = requestAnimationFrame(frame);
  }

  function start() {
    if (running || !gl) return;
    running = true;
    startedAt = performance.now();
    raf = requestAnimationFrame(frame);
  }

  function stop() {
    running = false;
    cancelAnimationFrame(raf);
  }

  function retint() {
    if (!probe) return;
    tint = readToken(probe, "--ray");
    fill = readToken(probe, "--ray2");
    if (gl && program) {
      gl.useProgram(program);
      gl.uniform3fv(uniforms.raysColor, tint);
      gl.uniform3fv(uniforms.fillColor, fill);
    }
  }

  return {
    boot() {
      // Reduced motion means no canvas at all — the CSS layers alone are the stage, and they are static.
      if (matchMedia("(prefers-reduced-motion: reduce)").matches) {
        canvas.style.display = "none";
        return;
      }
      if (!init()) {
        canvas.style.display = "none";
        return;
      }
      retint();
      resize();
      start();
      window.addEventListener("resize", resize, { passive: true });
      document.addEventListener("visibilitychange", () => {
        // A hidden tab runs no shader: the loop stops rather than burning a core in the background.
        if (document.hidden) stop();
        else start();
      });
    },
    stop,
    pointer(x, y) {
      mouse[0] = x / window.innerWidth;
      mouse[1] = y / window.innerHeight;
    },
    retint,
  };
}
