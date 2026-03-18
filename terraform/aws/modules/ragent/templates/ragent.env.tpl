%{ for key, value in environment_variables ~}
${key}=${jsonencode(value)}
%{ endfor ~}
