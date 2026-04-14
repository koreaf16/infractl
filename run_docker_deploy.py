import paramiko

host = "192.168.0.3"
port = 2222
username = "koreaf16"
password = "Gnttkak1!"

try:
    client = paramiko.SSHClient()
    client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
    client.connect(host, port=port, username=username, password=password, timeout=10)
    
    script_content = """#!/bin/bash
echo "Removing any existing vllm_gpu2 container..."
docker rm -f vllm_gpu2 2>/dev/null

echo "Starting vLLM using Docker on GPU 2 with FP8 context..."
docker run -d --name vllm_gpu2 \\
  --gpus '"device=2"' \\
  -v $HOME/.cache/huggingface:/root/.cache/huggingface \\
  -p 8001:8000 \\
  --ipc=host \\
  vllm/vllm-openai:latest \\
  --model Qwen/Qwen2.5-3B-Instruct-AWQ \\
  --quantization awq \\
  --kv-cache-dtype fp8 \\
  --max-model-len 65536 \\
  --gpu-memory-utilization 0.9

echo "Docker container started."
"""
    
    sftp = client.open_sftp()
    with sftp.file('deploy_docker_vllm.sh', 'w') as f:
        f.write(script_content)
    sftp.close()
    
    print("Executing docker deploy script on remote...")
    stdin, stdout, stderr = client.exec_command("bash deploy_docker_vllm.sh")
    print(stdout.read().decode())
    print(stderr.read().decode())
    
    client.close()
except Exception as e:
    print(f"Failed to connect or execute command: {e}")
