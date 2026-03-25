#!/usr/bin/env python3
# Copyright 2026 The RBG Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""render-rbg: Run aiconfigurator and render RBG deployment YAML files.

This script is intended to run inside the aiconfigurator container.
It performs the full pipeline:
  1. Execute aiconfigurator to generate configs
  2. Locate the output directory
  3. Parse generator_config.yaml for agg and disagg modes
  4. Render RBG-compatible YAML files into --output-dir
"""

import argparse
import os
import shutil
import re
import secrets
import string
import subprocess
import sys
from pathlib import Path

import yaml


# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------

BACKEND_SGLANG = "sglang"
BACKEND_VLLM = "vllm"
BACKEND_TRTLLM = "trtllm"

MODE_AGG = "agg"
MODE_DISAGG = "disagg"

RBG_API_VERSION = "workloads.x-k8s.io/v1alpha2"
RBG_KIND = "RoleBasedGroup"

SGLANG_DEFAULT_IMAGE = "lmsysorg/sglang:latest"
VLLM_DEFAULT_IMAGE = "vllm/vllm-openai:latest"
TRTLLM_DEFAULT_IMAGE = "nvcr.io/nvidia/ai-dynamo/tensorrtllm-runtime:latest"


# ---------------------------------------------------------------------------
# Argument parsing
# ---------------------------------------------------------------------------

def parse_args():
    parser = argparse.ArgumentParser(
        description="Run aiconfigurator and render RBG deployment YAML files",
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )

    # aiconfigurator pass-through parameters
    parser.add_argument("--model", required=True, help="Model name (e.g. Qwen/Qwen3-32B)")
    parser.add_argument("--system", required=True, help="GPU system type (e.g. h200_sxm)")
    parser.add_argument("--total-gpus", required=True, type=int, help="Total number of GPUs")
    parser.add_argument("--backend", default=BACKEND_SGLANG, help="Inference backend (sglang, vllm, trtllm)")
    parser.add_argument("--isl", required=True, type=int, help="Input sequence length")
    parser.add_argument("--osl", required=True, type=int, help="Output sequence length")
    parser.add_argument("--ttft", type=float, default=-1, help="Time to first token (ms)")
    parser.add_argument("--tpot", type=float, default=-1, help="Time per output token (ms)")
    parser.add_argument("--request-latency", type=float, default=-1, help="End-to-end request latency (ms)")
    parser.add_argument("--decode-system", default="", help="GPU system for decode workers")
    parser.add_argument("--backend-version", default="", help="Backend version")
    parser.add_argument("--prefix", type=int, default=0, help="Prefix cache length")
    parser.add_argument("--database-mode", default="SILICON", help="Database mode")

    # render-rbg specific parameters
    parser.add_argument("--model-path", required=True,
                        help="Model path inside the container (storage mount path)")
    parser.add_argument("--output-dir", required=True,
                        help="Directory to write rendered YAML files (under storage mount)")
    parser.add_argument("--image", default="",
                        help="Override container image for inference workers")
    parser.add_argument("--namespace", default="default",
                        help="Kubernetes namespace for generated resources")

    return parser.parse_args()


# ---------------------------------------------------------------------------
# aiconfigurator execution
# ---------------------------------------------------------------------------

# aiconfigurator's safe_mkdir only allows writes under a fixed set of prefixes:
# cwd, home, /tmp, /workspace, /var/tmp, tempfile.gettempdir().
# PVC mount paths (e.g. /models) are not in this list, so we must direct
# aiconfigurator to write under /workspace and then move the result to the
# actual output_dir on the PVC.
_AICONFIGURATOR_WHITELIST_PREFIXES = ("/tmp", "/workspace", "/var/tmp")
_AICONFIGURATOR_FALLBACK_WORK_DIR = "/workspace/rbg-generate-tmp"


def build_aiconfigurator_args(args, save_dir: str) -> list:
    """Build the aiconfigurator CLI arguments from parsed args."""
    cmd = ["aiconfigurator", "cli", "default"]
    cmd += ["--model", args.model]
    cmd += ["--system", args.system]
    cmd += ["--total-gpus", str(args.total_gpus)]
    cmd += ["--backend", args.backend]
    cmd += ["--isl", str(args.isl)]
    cmd += ["--osl", str(args.osl)]
    cmd += ["--ttft", str(args.ttft)]
    cmd += ["--tpot", str(args.tpot)]
    cmd += ["--database-mode", args.database_mode]
    cmd += ["--save-dir", save_dir]

    if args.decode_system and args.decode_system != args.system:
        cmd += ["--decode-system", args.decode_system]
    if args.backend_version:
        cmd += ["--backend-version", args.backend_version]
    if args.prefix > 0:
        cmd += ["--prefix", str(args.prefix)]
    if args.request_latency > 0:
        cmd += ["--request-latency", str(args.request_latency)]

    return cmd


def is_whitelisted_path(path: str) -> bool:
    """Return True if path is under aiconfigurator's safe_mkdir whitelist."""
    abs_path = os.path.abspath(path)
    for prefix in _AICONFIGURATOR_WHITELIST_PREFIXES:
        if abs_path.startswith(prefix + "/") or abs_path == prefix:
            return True
    return False


def get_work_dir(output_dir: str) -> str:
    """Determine the working directory for aiconfigurator.

    If output_dir is already under a whitelisted path (/tmp, /workspace, etc.),
    use it directly to avoid unnecessary copy. Otherwise, fall back to
    /workspace/rbg-generate-tmp and copy results afterwards.
    """
    if is_whitelisted_path(output_dir):
        return output_dir
    return _AICONFIGURATOR_FALLBACK_WORK_DIR


def run_aiconfigurator(args, work_dir: str):
    """Execute aiconfigurator under the given work_dir (must be whitelisted).

    aiconfigurator's safe_mkdir restricts writes to a fixed whitelist.
    The caller must ensure work_dir is under /tmp, /workspace, /var/tmp, etc.

    A timeout of 10 minutes is enforced to prevent the process from hanging
    indefinitely. stdout/stderr are captured so that failures produce actionable
    output in the container logs.
    """
    _AICONFIGURATOR_TIMEOUT_SECONDS = 600  # 10 minutes

    os.makedirs(work_dir, exist_ok=True)

    cmd = build_aiconfigurator_args(args, save_dir=work_dir)
    print(f"[render-rbg] Running: {' '.join(cmd)}", flush=True)
    try:
        result = subprocess.run(
            cmd,
            capture_output=True,
            text=True,
            timeout=_AICONFIGURATOR_TIMEOUT_SECONDS,
        )
    except subprocess.TimeoutExpired:
        print(
            f"[render-rbg] ERROR: aiconfigurator timed out after {_AICONFIGURATOR_TIMEOUT_SECONDS}s",
            file=sys.stderr,
        )
        sys.exit(1)

    if result.stdout:
        print(result.stdout, end="", flush=True)
    if result.stderr:
        print(result.stderr, end="", file=sys.stderr, flush=True)

    if result.returncode != 0:
        print(f"[render-rbg] ERROR: aiconfigurator exited with code {result.returncode}", file=sys.stderr)
        sys.exit(result.returncode)
    print("[render-rbg] aiconfigurator completed successfully", flush=True)


# ---------------------------------------------------------------------------
# Output directory location (mirrors parser.go LocateOutputDirectory)
# ---------------------------------------------------------------------------

def is_hugging_face_id(model_name: str) -> bool:
    """Return True if model_name looks like a HuggingFace ID (not a local path)."""
    if not model_name:
        return False
    if model_name.startswith("/"):
        return False
    if model_name.startswith("./") or model_name.startswith("../"):
        return False
    if len(model_name) >= 2 and model_name[1] == ":" and model_name[0].isalpha():
        return False
    if "\\" in model_name:
        return False
    return True


def get_model_base_name(model_name: str) -> str:
    """Extract the base name from a model name or path."""
    base = os.path.basename(model_name.rstrip("/"))
    return base if base not in (".", "") else ""


def locate_output_directory(work_dir: str, args) -> str:
    """Find the latest aiconfigurator output directory under work_dir.

    Mirrors the logic in parser.go LocateOutputDirectory.
    """
    model_name = args.model
    search_dir = work_dir

    if is_hugging_face_id(model_name):
        parts = model_name.split("/")
        model_base_name = parts[-1]
        if len(parts) > 1:
            # org subdirectory: {save-dir}/{org}/
            search_dir = os.path.join(search_dir, *parts[:-1])
    else:
        model_base_name = get_model_base_name(model_name)

    # Build prefix: {model}_{system}_{backend}_isl{isl}_osl{osl}_ttft{ttft}_tpot{tpot}_
    prefix = (
        f"{model_base_name}_{args.system}_{args.backend}"
        f"_isl{args.isl}_osl{args.osl}"
        f"_ttft{int(args.ttft)}_tpot{int(args.tpot)}_"
    )

    print(f"[render-rbg] Searching in: {search_dir}", flush=True)
    print(f"[render-rbg] Looking for prefix: {prefix}*", flush=True)

    try:
        entries = os.listdir(search_dir)
    except FileNotFoundError:
        print(f"[render-rbg] ERROR: search directory not found: {search_dir}", file=sys.stderr)
        sys.exit(1)

    matching = sorted(
        [
            os.path.join(search_dir, e)
            for e in entries
            if e.startswith(prefix) and os.path.isdir(os.path.join(search_dir, e))
        ],
        key=lambda p: os.path.getmtime(p),
        reverse=True,
    )

    if not matching:
        print(
            f"[render-rbg] ERROR: no output directory found in {search_dir} matching {prefix}*",
            file=sys.stderr,
        )
        sys.exit(1)

    latest = matching[0]
    print(f"[render-rbg] Found output directory: {latest}", flush=True)

    # Verify required subdirectories exist
    for sub in ("agg", "disagg"):
        if not os.path.isdir(os.path.join(latest, sub)):
            print(
                f"[render-rbg] ERROR: output directory missing required subdirectory: {sub}",
                file=sys.stderr,
            )
            sys.exit(1)

    return latest


# ---------------------------------------------------------------------------
# Config parsing (mirrors parser.go ParseGeneratorConfigs)
# ---------------------------------------------------------------------------

def parse_generator_config(config_path: str) -> dict:
    """Parse a generator_config.yaml file."""
    print(f"[render-rbg] Parsing: {config_path}", flush=True)
    with open(config_path) as f:
        cfg = yaml.safe_load(f)
    if not cfg:
        print(f"[render-rbg] ERROR: empty config at {config_path}", file=sys.stderr)
        sys.exit(1)
    mode = (cfg.get("DynConfig") or {}).get("mode", "")
    if not mode:
        print(f"[render-rbg] ERROR: missing DynConfig.mode in {config_path}", file=sys.stderr)
        sys.exit(1)
    return cfg


def parse_generator_configs(output_dir: str):
    """Parse both agg and disagg generator configs. Returns (agg_cfg, disagg_cfg)."""
    agg_path = os.path.join(output_dir, "agg", "top1", "generator_config.yaml")
    disagg_path = os.path.join(output_dir, "disagg", "top1", "generator_config.yaml")
    return parse_generator_config(agg_path), parse_generator_config(disagg_path)


# ---------------------------------------------------------------------------
# Name normalization
# ---------------------------------------------------------------------------

def normalize_model_name(name: str) -> str:
    """Convert model name to a valid Kubernetes resource name."""
    base = get_model_base_name(name)
    if not base:
        base = "rbg"
    result = []
    for c in base:
        if c.isdigit() or c.islower():
            result.append(c)
        elif c.isupper():
            result.append(c.lower())
        elif c in ("-", "_", "."):
            result.append("-")
    return "".join(result)


def generate_random_suffix(length: int = 5) -> str:
    """Generate a random lowercase hex suffix."""
    return secrets.token_hex((length + 1) // 2)[:length]


def get_deploy_name(model_name: str, backend: str, suffix: str) -> str:
    return f"{normalize_model_name(model_name)}-{backend}-{suffix}-{generate_random_suffix(5)}"


# ---------------------------------------------------------------------------
# Worker params extraction (mirrors types.go GetWorkerParams)
# ---------------------------------------------------------------------------

def get_worker_params(params: dict) -> dict:
    """Extract parallelization parameters from a params map."""
    return {
        "tensor_parallel_size": int(params.get("tensor_parallel_size", 0)),
        "pipeline_parallel_size": int(params.get("pipeline_parallel_size", 0)),
        "data_parallel_size": int(params.get("data_parallel_size", 0)),
        "moe_tensor_parallel_size": int(params.get("moe_tensor_parallel_size", 0)),
        "moe_expert_parallel_size": int(params.get("moe_expert_parallel_size", 0)),
    }


# ---------------------------------------------------------------------------
# Image selection (mirrors renderer.go getImage)
# ---------------------------------------------------------------------------

def get_image(backend: str, image_override: str = "") -> str:
    if image_override:
        return image_override
    return {
        BACKEND_SGLANG: SGLANG_DEFAULT_IMAGE,
        BACKEND_VLLM: VLLM_DEFAULT_IMAGE,
        BACKEND_TRTLLM: TRTLLM_DEFAULT_IMAGE,
    }.get(backend, SGLANG_DEFAULT_IMAGE)


# ---------------------------------------------------------------------------
# YAML building helpers
# ---------------------------------------------------------------------------

def pod_ip_env():
    return {"name": "POD_IP", "valueFrom": {"fieldRef": {"fieldPath": "status.podIP"}}}


def http_port():
    return {"containerPort": 8000, "name": "http"}


def readiness_probe():
    return {
        "initialDelaySeconds": 30,
        "periodSeconds": 10,
        "tcpSocket": {"port": 8000},
    }


def gpu_resources(count: int) -> dict:
    q = str(count)
    return {
        "limits": {"nvidia.com/gpu": q},
        "requests": {"nvidia.com/gpu": q},
    }


def model_volume_mount(model_path: str) -> dict:
    return {"name": "model", "mountPath": model_path}


def shm_volume_mount() -> dict:
    return {"name": "shm", "mountPath": "/dev/shm"}


def model_volume(model_name: str) -> dict:
    return {
        "name": "model",
        "persistentVolumeClaim": {"claimName": normalize_model_name(model_name)},
    }


def shm_volume(with_size_limit: bool) -> dict:
    empty_dir = {"medium": "Memory"}
    if with_size_limit:
        empty_dir["sizeLimit"] = "30Gi"
    return {"name": "shm", "emptyDir": empty_dir}


# ---------------------------------------------------------------------------
# SGLang command builder (mirrors renderer.go buildSGLangCommand)
# ---------------------------------------------------------------------------

def build_sglang_command(model_path: str, params: dict, disagg_mode: str = "") -> list:
    args = [
        "python3", "-m", "sglang.launch_server",
        "--model-path", model_path,
        "--enable-metrics",
    ]
    if disagg_mode:
        args += ["--disaggregation-mode", disagg_mode]
    args += ["--port", "8000", "--host", "$(POD_IP)"]

    if params["tensor_parallel_size"] > 0:
        args += ["--tensor-parallel-size", str(params["tensor_parallel_size"])]
    if params["pipeline_parallel_size"] > 0:
        args += ["--pipeline-parallel-size", str(params["pipeline_parallel_size"])]
    if params["data_parallel_size"] > 0:
        args += ["--data-parallel-size", str(params["data_parallel_size"])]
    if params["moe_expert_parallel_size"] > 0:
        args += ["--expert-parallel-size", str(params["moe_expert_parallel_size"])]
    if params["moe_tensor_parallel_size"] > 0:
        args += ["--moe-dense-tp-size", str(params["moe_tensor_parallel_size"])]

    return args


def build_unsupported_backend_command(backend: str) -> list:
    """Fallback command for unsupported backends (mirrors Go's echo fallback)."""
    return ["echo", f"Backend {backend} not yet supported"]


def build_worker_container(name: str, image: str, command: list, tp_size: int, model_path: str) -> dict:
    return {
        "name": name,
        "image": image,
        "imagePullPolicy": "Always",
        "env": [pod_ip_env()],
        "command": command,
        "ports": [http_port()],
        "readinessProbe": readiness_probe(),
        "resources": gpu_resources(tp_size),
        "volumeMounts": [model_volume_mount(model_path), shm_volume_mount()],
    }


def build_worker_pod_template(container: dict, model_name: str, with_shm_limit: bool) -> dict:
    return {
        "spec": {
            "volumes": [model_volume(model_name), shm_volume(with_shm_limit)],
            "containers": [container],
        }
    }


def build_role(name: str, replicas: int, pod_template: dict, dependencies: list = None) -> dict:
    role = {
        "name": name,
        "replicas": replicas,
        "standalonePattern": {"template": pod_template},
    }
    if dependencies:
        role["dependencies"] = dependencies
    return role


# ---------------------------------------------------------------------------
# Disagg YAML builder (mirrors renderer.go renderDisaggYAML)
# ---------------------------------------------------------------------------

def build_disagg_rbg(base_name: str, model_name: str, model_path: str,
                     backend: str, image: str, cfg: dict, namespace: str) -> dict:
    if backend != BACKEND_SGLANG:
        print(f"[render-rbg] ERROR: Router role configuration for backend {backend} not implemented",
              file=sys.stderr)
        sys.exit(1)

    workers = cfg.get("WorkerConfig", {})
    params = cfg.get("params", {})
    prefill_params = get_worker_params(params.get("prefill") or {})
    decode_params = get_worker_params(params.get("decode") or {})

    prefill_replicas = workers.get("prefill_workers", 0)
    decode_replicas = workers.get("decode_workers", 0)

    # Router role
    router_cmd = [
        "python3", "-m", "sglang_router.launch_router",
        "--pd-disaggregation",
    ]
    for i in range(prefill_replicas):
        router_cmd += [
            "--prefill",
            f"http://{base_name}-prefill-{i}.s-{base_name}-prefill:8000",
        ]
    for i in range(decode_replicas):
        router_cmd += [
            "--decode",
            f"http://{base_name}-decode-{i}.s-{base_name}-decode:8000",
        ]
    router_cmd += ["--host", "0.0.0.0", "--port", "8000"]

    router_container = {
        "name": "schedule",
        "image": image,
        "imagePullPolicy": "Always",
        "command": router_cmd,
        "volumeMounts": [model_volume_mount(model_path)],
    }
    router_pod = {
        "spec": {
            "volumes": [model_volume(model_name)],
            "containers": [router_container],
        }
    }
    router_role = build_role("router", 1, router_pod, dependencies=["prefill", "decode"])

    # Prefill role
    if backend == BACKEND_SGLANG:
        prefill_cmd = build_sglang_command(model_path, prefill_params, "prefill")
    else:
        prefill_cmd = build_unsupported_backend_command(backend)
    prefill_container = build_worker_container(
        f"{backend}-prefill", image, prefill_cmd, prefill_params["tensor_parallel_size"], model_path
    )
    prefill_pod = build_worker_pod_template(prefill_container, model_name, with_shm_limit=True)
    prefill_role = build_role("prefill", prefill_replicas, prefill_pod)

    # Decode role
    if backend == BACKEND_SGLANG:
        decode_cmd = build_sglang_command(model_path, decode_params, "decode")
    else:
        decode_cmd = build_unsupported_backend_command(backend)
    decode_container = build_worker_container(
        f"{backend}-decode", image, decode_cmd, decode_params["tensor_parallel_size"], model_path
    )
    decode_pod = build_worker_pod_template(decode_container, model_name, with_shm_limit=True)
    decode_role = build_role("decode", decode_replicas, decode_pod)

    return {
        "apiVersion": RBG_API_VERSION,
        "kind": RBG_KIND,
        "metadata": {"name": base_name, "namespace": namespace},
        "spec": {"roles": [router_role, prefill_role, decode_role]},
    }


# ---------------------------------------------------------------------------
# Agg YAML builder (mirrors renderer.go renderAggYAML)
# ---------------------------------------------------------------------------

def build_agg_rbg(base_name: str, model_name: str, model_path: str,
                  backend: str, image: str, cfg: dict, namespace: str) -> dict:
    workers = cfg.get("WorkerConfig", {})
    params = cfg.get("params", {})
    agg_params = get_worker_params(params.get("agg") or {})

    agg_replicas = workers.get("agg_workers", 1)

    if backend == BACKEND_SGLANG:
        agg_cmd = build_sglang_command(model_path, agg_params)
    else:
        agg_cmd = build_unsupported_backend_command(backend)
    agg_container = build_worker_container(
        f"{backend}-worker", image, agg_cmd, agg_params["tensor_parallel_size"], model_path
    )
    agg_pod = build_worker_pod_template(agg_container, model_name, with_shm_limit=False)
    agg_role = build_role("worker", agg_replicas, agg_pod)

    return {
        "apiVersion": RBG_API_VERSION,
        "kind": RBG_KIND,
        "metadata": {"name": base_name, "namespace": namespace},
        "spec": {"roles": [agg_role]},
    }


# ---------------------------------------------------------------------------
# Service builder (mirrors renderer.go buildServiceSpec)
# ---------------------------------------------------------------------------

def build_service(name: str, target_role: str, namespace: str) -> dict:
    return {
        "apiVersion": "v1",
        "kind": "Service",
        "metadata": {
            "name": name,
            "namespace": namespace,
            "labels": {"app": name},
        },
        "spec": {
            "type": "ClusterIP",
            "ports": [{"name": "http", "port": 8000, "protocol": "TCP", "targetPort": 8000}],
            "selector": {
                "rolebasedgroup.workloads.x-k8s.io/name": name,
                "rolebasedgroup.workloads.x-k8s.io/role": target_role,
            },
        },
    }


# ---------------------------------------------------------------------------
# YAML serialization
# ---------------------------------------------------------------------------

def dump_multi_doc(*docs) -> str:
    """Serialize multiple dicts as a multi-document YAML string.

    Matches Go marshalMultiDocYAML: the first document has no leading separator;
    subsequent documents are preceded by '---\n'.
    """
    parts = []
    for i, doc in enumerate(docs):
        serialized = yaml.dump(doc, default_flow_style=False, allow_unicode=True)
        if i == 0:
            parts.append(serialized)
        else:
            parts.append("---\n" + serialized)
    return "".join(parts)


# ---------------------------------------------------------------------------
# Rendering (mirrors renderer.go RenderDeploymentYAML)
# ---------------------------------------------------------------------------

def render_and_write(path: str, content: str):
    parent = os.path.dirname(path)
    if parent:
        os.makedirs(parent, exist_ok=True)
    with open(path, "w") as f:
        f.write(content)
    print(f"[render-rbg] Written: {path}", flush=True)


def render_disagg(output_dir: str, model_name: str, model_path: str,
                  backend: str, image: str, cfg: dict, namespace: str):
    base_name = get_deploy_name(model_name, backend, "pd")
    rbg = build_disagg_rbg(base_name, model_name, model_path, backend, image, cfg, namespace)
    svc = build_service(base_name, "router", namespace)
    content = dump_multi_doc(rbg, svc)
    out_path = os.path.join(output_dir, f"{normalize_model_name(model_name)}-{backend}-disagg.yaml")
    render_and_write(out_path, content)
    return base_name


def render_agg(output_dir: str, model_name: str, model_path: str,
               backend: str, image: str, cfg: dict, namespace: str):
    base_name = get_deploy_name(model_name, backend, "agg")
    rbg = build_agg_rbg(base_name, model_name, model_path, backend, image, cfg, namespace)
    svc = build_service(base_name, "worker", namespace)
    content = dump_multi_doc(rbg, svc)
    out_path = os.path.join(output_dir, f"{normalize_model_name(model_name)}-{backend}-agg.yaml")
    render_and_write(out_path, content)
    return base_name


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

def main():
    args = parse_args()
    # Determine working directory: if output_dir is under /tmp or /workspace,
    # write directly there; otherwise use /workspace fallback and copy later.
    work_dir = get_work_dir(args.output_dir)
    if work_dir == args.output_dir:
        print(f"[render-rbg] Output dir {args.output_dir} is whitelisted; writing directly", flush=True)
    else:
        print(f"[render-rbg] Output dir {args.output_dir} is not whitelisted; using fallback {work_dir}", flush=True)

    # Step 1: Run aiconfigurator (writes to work_dir, which may be output_dir or fallback)
    print("[render-rbg] === Step 1: Running aiconfigurator ===", flush=True)
    run_aiconfigurator(args, work_dir)

    # Step 2: Locate output directory (inside work_dir)
    print("[render-rbg] === Step 2: Locating output directory ===", flush=True)
    gen_output_dir = locate_output_directory(work_dir, args)

    # Step 3: Parse configs
    print("[render-rbg] === Step 3: Parsing generator configs ===", flush=True)
    agg_cfg, disagg_cfg = parse_generator_configs(gen_output_dir)

    # Step 4: Render YAML files into work_dir (still in /workspace)
    print("[render-rbg] === Step 4: Rendering RBG YAML files ===", flush=True)
    image = get_image(args.backend, args.image)

    render_disagg(work_dir, args.model, args.model_path, args.backend, image, disagg_cfg, args.namespace)
    render_agg(work_dir, args.model, args.model_path, args.backend, image, agg_cfg, args.namespace)

    # Step 5: Copy files from work_dir to output_dir if they differ.
    # If work_dir == output_dir (whitelisted path), this step is skipped.
    if work_dir != args.output_dir:
        print("[render-rbg] === Step 5: Copying output ===", flush=True)
        for src_dir, _dirs, files in os.walk(work_dir):
            rel = os.path.relpath(src_dir, work_dir)
            dst_dir = os.path.join(args.output_dir, rel) if rel != "." else args.output_dir
            os.makedirs(dst_dir, exist_ok=True)
            for filename in files:
                shutil.copy2(os.path.join(src_dir, filename), os.path.join(dst_dir, filename))
        print(f"[render-rbg] Output files written to: {args.output_dir}", flush=True)
    else:
        print(f"[render-rbg] Output files already in: {args.output_dir}", flush=True)

    print("[render-rbg] === Done ===", flush=True)


if __name__ == "__main__":
    main()
