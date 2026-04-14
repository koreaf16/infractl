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
# The huggingface-cli should be in the venv bin folder
nohup /home/vllm/comfy/venv/bin/huggingface-cli download Qwen/Qwen2.5-3B-Instruct-AWQ > dl_awq.log 2>&1 &
"""
    
    sftp = client.open_sftp()
    with sftp.file('download.sh', 'w') as f:
        f.write(script_content)
    sftp.close()
    
    print("Executing bash script to download using venv huggingface-cli...")
    stdin, stdout, stderr = client.exec_command("bash download.sh")
    print(stdout.read().decode())
    print(stderr.read().decode())
    
    client.close()
except Exception as e:
    print(f"Failed to connect or execute command: {e}")
