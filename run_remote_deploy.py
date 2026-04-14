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
export PATH=$HOME/.local/bin:$PATH
echo "Creating new virtual environment for vLLM..."
/usr/bin/python3 -m venv $HOME/vllm-venv
source $HOME/vllm-venv/bin/activate
echo "Installing vLLM (this might take a few minutes)..."
pip install --upgrade pip
pip install vllm
echo "Starting vLLM on GPU 2 with FP8 context..."
CUDA_VISIBLE_DEVICES=2 nohup python -m vllm.entrypoints.openai.api_server \
  --model Qwen/Qwen2.5-3B-Instruct-AWQ \
  --quantization awq \
  --kv-cache-dtype fp8 \
  --max-model-len 65536 \
  --host 0.0.0.0 \
  --port 8001 \
  --gpu-memory-utilization 0.9 > vllm_gpu2.log 2>&1 &
echo $! > vllm_gpu2.pid
echo "vLLM installation and startup in background completed."
"""
    
    sftp = client.open_sftp()
    with sftp.file('deploy_vllm_gpu2.sh', 'w') as f:
        f.write(script_content)
    sftp.close()
    
    print("Executing deploy script on remote...")
    stdin, stdout, stderr = client.exec_command("bash deploy_vllm_gpu2.sh")
    print(stdout.read().decode())
    print(stderr.read().decode())
    
    client.close()
except Exception as e:
    print(f"Failed to connect or execute command: {e}")
